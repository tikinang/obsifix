# Obsifix
> Transformation tool of Obsidian .md files to Quartz-compatible format.

Basically adding `title` and `lastmod` frontmatter attributes, and little formatting.
There are further feature possibilities, such as preprocessing of embedded linked content, that are hard to implement solely in hugo templates, as done in Quartz :/
**Written in Go!**

## Usage
```
Usage of obsifix:
  -clean
        Remove all files and dirs from target, but only if target differs from working dir.
  -debug
        Print debug information.
  -force
        Execute all changes without asking.
  -git-chtime
        Change files chtime from git, useful right after git clone.
  -quartz
        Prepare frontmatter for Quartz publishing.
  -reformat
        Replace frontmatter with this tool format and fix ending newlines.
  -target string
        Path to write changed files to. (default "/home/tikinang/code/go/obsifix")
```
