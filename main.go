// Package main is a placeholder that keeps the pipeline green from first clone
// and demonstrates the house test style. Replace it with your project.
package main

import "fmt"

// Greet exists so main_test.go has something table-driven to test.
func Greet(name string) string {
	if name == "" {
		name = "world"
	}
	return "hello, " + name
}

func main() {
	fmt.Println(Greet(""))
}
