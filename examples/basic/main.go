package main

import (
    webview "github.com/tester305/webview_go"
    "log"
)

func main() {
    w := webview.New(false)
    if w == nil {
        log.Fatal("Failed to initialize webview: returned nil")
    }
    defer w.Destroy()

    w.SetTitle("Basic Example")
    w.SetSize(480, 320, webview.HintNone)
    w.SetHtml("If you see this that means the webview succeeded, you can use me now.")
    w.Run()
}

