package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

type Datetime struct {
	time.Time
}

const datetimeFormat = "Monday, 2 January 2006 15:04:05"

func (r *Datetime) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var buf string
	err := unmarshal(&buf)
	if err != nil {
		return nil
	}
	tt, err := time.Parse(datetimeFormat, strings.TrimSpace(buf))
	if err != nil {
		r.Time = time.Time{}
	}
	r.Time = tt
	return nil
}

func (r Datetime) MarshalYAML() (any, error) {
	return r.Time.Format(datetimeFormat), nil
}

type MatterIn struct {
	Created Datetime `yaml:"created"`
	Tags    []string `yaml:"tags,omitempty"`
	Aliases []string `yaml:"aliases,omitempty"`
	Publish bool     `yaml:"publish,omitempty"`
}

type MatterOut struct {
	Created time.Time `yaml:"created"`
	Lastmod time.Time `yaml:"lastmod"`
	Title   string    `yaml:"title"`
	Tags    []string  `yaml:"tags"`
	Aliases []string  `yaml:"aliases"`
}

func main() {

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	var target string
	var force, quartz, fixChtimeFromGit, reformat, debug, clean bool

	flag.StringVar(&target, "target", wd, "Path to write changed files to.")
	flag.BoolVar(&force, "force", false, "Execute all changes without asking.")
	flag.BoolVar(&debug, "debug", false, "Print debug information.")
	flag.BoolVar(&clean, "clean", false, "Remove all files and dirs from target, but only if target differs from working dir.")
	flag.BoolVar(&quartz, "quartz", false, "Prepare frontmatter for Quartz publishing.")
	flag.BoolVar(&reformat, "reformat", false, "Replace frontmatter with this tool format and fix ending newlines.")
	flag.BoolVar(&fixChtimeFromGit, "git-chtime", false, "Change files chtime from git, useful right after git clone.")
	flag.Parse()

	if target != wd && clean {
		if err := os.RemoveAll(target); err != nil {
			panic(err)
		}
	}

	inputs := make(chan bool)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			switch scanner.Text() {
			case "y":
				inputs <- true
			default:
				inputs <- false
			}
		}
	}()

	if err := filepath.Walk(
		wd,
		func(fpath string, info fs.FileInfo, err error) error {
			if fixChtimeFromGit {
				gitTime, err := getGitLastMod(fpath)
				if err != nil {
					return err
				}
				if !gitTime.IsZero() && !gitTime.Equal(info.ModTime()) {
					fmt.Printf("Changing chtime: %s\n", info.Name())
					return os.Chtimes(fpath, gitTime, gitTime)
				}
				return nil
			}

			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}

			contentPath := strings.TrimPrefix(fpath, wd)
			if strings.HasPrefix(contentPath, "/templates") {
				if debug {
					fmt.Printf("Skipping processing file: %s\n", contentPath)
				}
				return nil
			}
			if strings.HasPrefix(contentPath, "/assets") {
				fmt.Printf("Copying file: %s\n", contentPath)
				return copy(fpath, path.Join(target, contentPath))
			}
			if debug {
				fmt.Printf("Processing file: %s\n", contentPath)
			}

			matterIn, content, err := getFrontMatterIn(fpath)
			if err != nil {
				return err
			}

			var matter any
			var always bool
			if reformat {
				for i, tag := range matterIn.Tags {
					if tag == "wip" {
						matterIn.Tags[i] = "draft"
					}
				}
				created, err := getGitCreated(fpath)
				if err != nil {
					return err
				}
				matterIn.Created = Datetime{created}

				matter = matterIn
				if force {
					goto compareAndWrite
				} else {
					fmt.Printf("Do you want to reformat file: %s (y/n)? ", contentPath)
					if <-inputs {
						goto compareAndWrite
					}
				}
			} else if quartz {
				if !matterIn.Publish {
					fmt.Printf("Not publishing: %s\n", contentPath)
					return nil
				}
				for _, tag := range matterIn.Tags {
					if tag == "draft" {
						fmt.Printf("Not publishing (due to draft tag): %s\n", contentPath)
						return nil
					}
				}
				// Fill Quartz-compatible frontmatter.
				matterOut := MatterOut{
					Title:   strings.TrimSuffix(info.Name(), ".md"),
					Aliases: matterIn.Aliases,
					Tags:    matterIn.Tags,
				}
				created, err := getGitCreated(fpath)
				if err != nil {
					return err
				}
				matterOut.Created = created
				lastmod, err := getGitLastMod(fpath)
				if err != nil {
					return err
				}
				matterOut.Lastmod = lastmod
				if contentPath == "/_index.md" {
					matterOut.Title = "Index"
				}

				matter = matterOut
				always = true
				if force {
					goto compareAndWrite
				} else {
					fmt.Printf("Do you want to Quartz fix file: %s (y/n)? ", contentPath)
					if <-inputs {
						goto compareAndWrite
					}
				}
			}

			return nil

		compareAndWrite:
			buf := bytes.NewBuffer(nil)
			fmt.Fprintln(buf, "---")
			if err := yaml.NewEncoder(buf).Encode(matter); err != nil {
				return err
			}
			fmt.Fprintln(buf, "---")
			content = bytes.TrimSpace(content)
			buf.Write(content)
			buf.WriteRune('\n')

			original, err := os.ReadFile(fpath)
			if err != nil {
				return err
			}

			writeFpath := path.Join(target, contentPath)
			writePath, _ := path.Split(writeFpath)
			if err := os.MkdirAll(writePath, 509); err != nil {
				return err
			}

			if bytes.Compare(buf.Bytes(), original) != 0 {
				fmt.Printf("Writing changed file: %s\n", contentPath)
				return os.WriteFile(writeFpath, buf.Bytes(), info.Mode())
			}
			if always {
				fmt.Printf("Writing file with original content: %s\n", contentPath)
				return os.WriteFile(writeFpath, original, info.Mode())
			}
			if debug {
				fmt.Printf("Skipping writing file: %s\n", contentPath)
			}

			return nil
		},
	); err != nil {
		panic(err)
	}
}

func getGitLastMod(path string) (time.Time, error) {
	cmd := exec.Command("git", "log", "-1", "--pretty=format:%ci", path)
	b, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}
	if len(b) == 0 {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(string(b)))
}

func copy(source, target string) error {
	f1, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f1.Close()

	f2, err := os.Create(target)
	if err != nil {
		return err
	}
	defer f2.Close()

	io.Copy(f2, f1)

	return nil
}

func getGitCreated(path string) (time.Time, error) {
	cmd := exec.Command("git", "log", "--diff-filter=A", "--follow", "--format=%ci", "-1", "--", path)
	b, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}
	if len(b) == 0 {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(string(b)))
}

func getFrontMatterIn(path string) (MatterIn, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return MatterIn{}, nil, err
	}
	defer f.Close()

	var matter MatterIn
	rest, err := frontmatter.Parse(f, &matter)
	if err != nil {
		return MatterIn{}, nil, err
	}

	return matter, rest, nil
}
