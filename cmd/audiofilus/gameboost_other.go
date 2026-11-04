//go:build !linux

package main

// Fuera de Linux no hay juegos que lanzar (el modo juego es de Knulli).

// boostGame no hace nada.
func boostGame() (restore func()) { return func() {} }

// thawLeftovers no hace nada.
func thawLeftovers() {}
