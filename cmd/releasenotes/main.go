// Command releasenotes extracts one version section from changelogs.md.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var semverTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func main() {
	tag := flag.String("tag", "", "version tag to extract, such as v1.2.3")
	file := flag.String("file", "changelogs.md", "changelog Markdown file")
	out := flag.String("out", "", "write notes to this file instead of stdout")
	flag.Parse()
	if !semverTag.MatchString(*tag) {
		fatalf("tag must use vMAJOR.MINOR.PATCH format")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		fatalf("read %s: %v", *file, err)
	}
	notes, err := extract(string(data), *tag)
	if err != nil {
		fatalf("%v", err)
	}
	if *out == "" {
		_, err = os.Stdout.WriteString(notes)
	} else {
		err = os.WriteFile(*out, []byte(notes), 0644)
	}
	if err != nil {
		fatalf("write release notes: %v", err)
	}
}

func extract(markdown, tag string) (string, error) {
	var section []string
	active, found := false, false
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## ") {
			if active {
				break
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			version := heading
			if strings.HasPrefix(version, "[") {
				if end := strings.IndexByte(version, ']'); end >= 0 {
					version = version[1:end]
				}
			} else if fields := strings.Fields(version); len(fields) > 0 {
				version = fields[0]
			}
			active = version == tag
			if active {
				found = true
			}
			continue
		}
		if active {
			section = append(section, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no ## %s section found in changelog", tag)
	}
	notes := strings.TrimSpace(strings.Join(section, "\n"))
	if notes == "" {
		return "", fmt.Errorf("changelog section for %s is empty", tag)
	}
	return notes + "\n", nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "releasenotes: "+format+"\n", args...)
	os.Exit(1)
}
