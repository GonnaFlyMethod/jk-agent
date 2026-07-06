package ui

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinnerColors = []int{213, 177, 141, 105, 69, 39, 45, 51, 87, 123}

// StartSpinner prints an animated braille spinner with a label, cycling
// colors, until the returned stop func is called — then it clears the
// line so whatever prints next (a reply, a tool result) starts clean.
// Safe to call stop more than once.
func StartSpinner(label string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-stop:
				fmt.Print("\r\033[K")
				return
			case <-ticker.C:
				frame := spinnerFrames[i%len(spinnerFrames)]
				color := spinnerColors[i%len(spinnerColors)]
				fmt.Printf("\r\033[38;5;%dm%s\033[0m %s", color, frame, label)
				i++
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}
