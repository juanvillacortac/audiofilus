package main

import (
	"fmt"
	"os"

	"github.com/juanvillacortac/audiofilus/internal/input"
	"github.com/juanvillacortac/audiofilus/internal/sdlmap"
)

// runPad muestra cómo ve SDL los mandos y qué mapeo usa audiofilus, para
// diagnosticar una consola nueva.
func runPad() int {
	ctrls, err := sdlmap.Controllers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "SDL:", err)
	}
	for _, c := range ctrls {
		fmt.Printf("SDL: %s  %s\n  %s\n", c.Name, c.Path, c.Mapping)
	}
	res, err := input.Resolve("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	fmt.Printf("audiofilus: %s %s (mapeo %s)\n  %s\n", res.Name, res.Path, res.Source, res.Layout.Describe())
	return 0
}
