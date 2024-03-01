// This is the target program that we will attach the uprobe to. It is a simple
// program that runs the `ls` command every 5 seconds and logs the output.

package main

import (
	"log"
	"time"
)

var iteration int = 0

func main() {
	for {
		log.Printf("Count: %d\n", getCount())
		time.Sleep(1 * time.Second)
	}
}

// Don't inline this function so that we can attach a uprobe to it.
//
//go:noinline
func getCount() int {
	iteration++
	return iteration
}
