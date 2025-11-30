package main

import webview "github.com/tester305/webview_go"

func main() {
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Google")
	w.SetSize(480, 320, webview.HintNone)

	// Navigate to Google instead of using embedded HTML
	w.Navigate("https://www.google.com")

	w.Run()
}
