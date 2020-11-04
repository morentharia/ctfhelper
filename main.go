package main

import (
	"fmt"
	"log"
	"os"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

const (
	ChromeURL = ":9222"
)

func main() {
	b := rod.New().ControlURL(launcher.MustResolveURL(ChromeURL))
	err := b.Connect()
	if err != nil {
		log.Fatal(err)
	}
	js := `function log(msg){fetch("/challengehelperlog?msg="+msg)}`

	go b.HijackRequests().MustAdd("*/challengehelperlog*", func(h *rod.Hijack) {
		fmt.Printf("%s\n", h.Request.URL().Query().Get("msg"))
		h.Response.SetBody("")
	}).Run()

	switch {
	case len(os.Args) == 2:
		targetID := os.Args[1]
		p, err := b.PageFromTarget(proto.TargetTargetID(targetID))
		if err != nil {
			logrus.WithField("TargetID", targetID).WithError(err).Error("b.PageFromTarget")
			return
		}
		fmt.Printf("%s\n", p.MustEval("document.documentElement.innerHTML").String())
	case len(os.Args) == 3:
		targetID := os.Args[1]
		newLoaction := os.Args[2]
		p, err := b.PageFromTarget(proto.TargetTargetID(targetID))
		if err != nil {
			logrus.WithError(err).Error("b.PageFromTarget")
			return
		}
		p.MustEvalOnNewDocument(js)
		p.Navigate(newLoaction)
		p.WaitLoad()
		fmt.Printf("%s\n", p.MustEval("document.documentElement.innerHTML").String())
	default:
		for i, p := range b.MustPages() {
			p.MustEvalOnNewDocument(js)
			// fmt.Printf("%d\t %s %s %s\n", i, p.TargetID, p.MustEval("()=>document.location.href"), p.MustEval("()=>document.title"))
			fmt.Printf("%-04d %s %s\n", i, p.TargetID, p.MustEval("()=>document.location.href"))
		}
	}
}
