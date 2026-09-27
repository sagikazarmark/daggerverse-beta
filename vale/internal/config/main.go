// Read only the sync inputs, using the same INI parser as Vale. Vale itself
// validates and resolves the complete lint configuration after synchronization.
package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/errata-ai/ini"
)

type config struct {
	StylesPath     string   `json:"stylesPath"`
	Packages       []string `json:"packages"`
	ConfigContents string   `json:"configContents"`
}

func read(src any) (config, error) {
	// Vale's full config loader permits inline comments only after whitespace.
	f, err := ini.LoadSources(ini.LoadOptions{SpaceBeforeInlineComment: true}, src)
	if err != nil {
		return config{}, err
	}
	section := f.Section("")
	// GetPackages uses the parser's default options, independently of the full loader.
	packages, err := ini.Load(src)
	if err != nil {
		return config{}, err
	}
	result := config{
		StylesPath: section.Key("StylesPath").String(),
		Packages:   packages.Section("").Key("Packages").Strings(","),
	}
	if result.StylesPath == "" {
		section.Key("StylesPath").SetValue("/work/styles")
		var contents strings.Builder
		if _, err := f.WriteTo(&contents); err != nil {
			return config{}, err
		}
		result.ConfigContents = contents.String()
	}
	return result, nil
}

func inspect(filename string) (config, error) {
	if !strings.HasSuffix(filename, ".zip") {
		return read(filename)
	}
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return config{}, err
	}
	defer archive.Close()
	// A package's config is directly inside its single top-level directory.
	for _, file := range archive.File {
		if path.Base(file.Name) != ".vale.ini" || strings.Count(file.Name, "/") != 1 {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return config{}, err
		}
		defer reader.Close()
		contents, err := io.ReadAll(reader)
		if err != nil {
			return config{}, err
		}
		return read(contents)
	}
	return config{Packages: []string{}}, nil
}

func main() {
	cfg, err := inspect(os.Args[1])
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(cfg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
