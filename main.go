package main

import (
	"bufio"
	"fmt"
	"os"
)

func setProgress(percent float64, text string, override bool) {
	if !override {
		fmt.Print(fmt.Sprintf("\n%.2f%% %s          ", percent*100, text))
	} else {
		fmt.Print(fmt.Sprintf("\r%.2f%% %s          ", percent*100, text))
	}
}

func main() {
	fmt.Println("Commencing update, please wait until file synchronization is complete.")
	synchronize()
	fmt.Println("\nComplete! <Press enter to quit>")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
