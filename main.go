package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

type Matter struct {
	Title   string    `yaml:"title,omitempty"`
	Aliases []string  `yaml:"aliases,omitempty"`
	Tags    []string  `yaml:"tags,omitempty"`
	Lastmod time.Time `yaml:"lastmod"`
	Draft   bool      `yaml:"draft"`
}

func main() {

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	var path string
	var force bool
	var replaceTitle bool
	var updateLastmod bool
	var fixChtimeFromGit bool
	var reformat bool

	flag.StringVar(&path, "path", wd, "Content root path (=obsidian vault path).")
	flag.BoolVar(&force, "force", false, "Execute all changes without asking.")
	flag.BoolVar(&replaceTitle, "title", false, "Replace frontmatter title with the file name.")
	flag.BoolVar(&updateLastmod, "lastmod", true, "Update frontmatter lastmod with current time.")
	flag.BoolVar(&fixChtimeFromGit, "git-chtime", false, "Change files chtime from git, useful right after git clone.")
	flag.BoolVar(&reformat, "reformat", false, "Replace frontmatter with this tool format.")
	flag.Parse()

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

	lastmod := time.Now().Truncate(time.Second)

	if err := filepath.Walk(
		wd,
		func(path string, info fs.FileInfo, err error) error {
			if fixChtimeFromGit {
				gitTime, err := getGitLastMod(path)
				if err != nil {
					return err
				}
				if !gitTime.IsZero() && !gitTime.Equal(info.ModTime()) {
					fmt.Printf("Changing chtime: %s\n", info.Name())
					return os.Chtimes(path, gitTime, gitTime)
				}
				return nil
			}

			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}
			if strings.Contains(path, "templates/") {
				return nil
			}

			var fileChanged bool
			title := strings.TrimSuffix(info.Name(), ".md")
			matter, content, err := getFrontMatter(path)
			if err != nil {
				return err
			}

			if replaceTitle && matter.Title != title {
				if force {
					matter.Lastmod = lastmod
					fileChanged = true
				} else {
					fmt.Printf("Do you want to change title: %s -> %s (y/n)? ", matter.Title, title)
					if <-inputs {
						matter.Title = title
						fileChanged = true
					}
				}
			}
			if updateLastmod {
				changed, err := getGitFileChanged(path)
				if err != nil {
					return err
				}
				if changed {
					if matter.Lastmod.IsZero() || !matter.Lastmod.Equal(lastmod) {
						if force {
							matter.Lastmod = lastmod
							fileChanged = true
						} else {
							fmt.Printf("Do you want to update lastmod for: %s (y/n)? ", title)
							if <-inputs {
								matter.Lastmod = lastmod
								fileChanged = true
							}
						}
					}
				}
			}

			if reformat {
				if force {
					goto write
				} else {
					fmt.Printf("Do you want to reformat file: %s (y/n)? ", title)
					if <-inputs {
						goto write
					}
				}
			}
			if fileChanged {
				goto write
			}

			return nil

		write:
			fmt.Printf("Writing file: %s\n", title)
			buf := bytes.NewBuffer(nil)
			fmt.Fprintln(buf, "---")
			yaml.NewEncoder(buf).Encode(matter)
			fmt.Fprintln(buf, "---")
			buf.Write(content)
			return os.WriteFile(path, buf.Bytes(), info.Mode())
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

func getGitFileChanged(path string) (bool, error) {
	cmd := exec.Command("git", "diff", "--cached", "--exit-code", "--no-patch", path)
	cmd.Run()
	return cmd.ProcessState.ExitCode() == 1, nil
}

func getFrontMatter(path string) (Matter, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return Matter{}, nil, err
	}
	defer f.Close()

	var matter Matter
	rest, err := frontmatter.Parse(f, &matter)
	if err != nil {
		return Matter{}, nil, err
	}

	return matter, rest, nil
}
