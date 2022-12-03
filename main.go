package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
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

type MatterIn struct {
	Aliases []string `yaml:"aliases,omitempty"`
	Tags    []string `yaml:"tags,omitempty"`
	Publish bool     `yaml:"publish,omitempty"`
}

type MatterOut struct {
	Title   string    `yaml:"title"`
	Aliases []string  `yaml:"aliases"`
	Tags    []string  `yaml:"tags"`
	Lastmod time.Time `yaml:"lastmod"`
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
	flag.BoolVar(&debug, "debug", false, "Print extra information.")
	flag.BoolVar(&clean, "clean", false, "Clean-up target dir.")
	flag.BoolVar(&quartz, "quartz", false, "Prepare frontmatter for Quartz publishing.")
	flag.BoolVar(&reformat, "reformat", false, "Replace frontmatter with this tool format and fix ending newline.")
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
				if strings.Contains(fpath, "templates/") {
					return nil
				}
				for i, tag := range matterIn.Tags {
					if tag == "wip" {
						matterIn.Tags[i] = "draft"
					}
				}
				if len(matterIn.Tags) == 0 {
					matterIn.Tags = append(matterIn.Tags, "draft")
				}

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
				lastmod, err := getGitLastMod(fpath)
				if err != nil {
					return err
				}
				matterOut.Lastmod = lastmod
				if contentPath == "/_index.md" {
					matterOut.Title = "Tikinang's 2nd 🧠"
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
			yaml.NewEncoder(buf).Encode(matter)
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
				fmt.Printf("Skipping file: %s\n", contentPath)
			}

			return nil
		},
	); err != nil {
		panic(err)
	}
}

func getGitLastMod(path string) (time.Time, error) {
	cmd := exec.Command("git", "log", "-1", "--pretty=format:%ci", path)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return time.Time{}, err
	}
	if len(b) == 0 {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:04:05 -0700", string(b))
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
