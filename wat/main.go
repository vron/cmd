// Package just modified from orginal package found at
// http://code.google.com/p/rsc
// Used to watch a directory and run a command when it changes (or one of the files in it)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"io/ioutil"

	"code.google.com/p/goplan9/plan9/acme"
	"github.com/howeyc/fsnotify"
)

var args []string
var win *acme.Win
var needrun = make(chan bool, 1)

var recursive = flag.Bool("r", false, "Activate recursive checking of subdirs, currently only 2 levels")

func main() {
	flag.Parse()
	args = flag.Args()
	if len(args) < 1 {
		fmt.Println(`Watch current dir and run the given command when something
changes, thus need at least one argument`)
		os.Exit(0)
	}

	var err error
	win, err = acme.New()
	if err != nil {
		log.Fatal(err)
	}
	pwd, _ := os.Getwd()
	win.Name(pwd + "/+watch")
	win.Ctl("clean")
	win.Fprintf("tag", "Get ")
	needrun <- true
	go events()
	go runner()

	// Init new watcher
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Fatal(err)
		}

	// Start listening for these events
	go func() {
		for {
			select {
			case _ = <-watcher.Event:
				needrun <- true
			case err := <-watcher.Error:
				log.Fatal("error:", err)
			}
		}
	}()
	var rset func(int, int, string)
	rset = func(curdepth, totdepth int, path string)   {
		err = watcher.Watch(path)
		
		if err != nil {
			log.Fatal(err)
		}
		if curdepth < totdepth {
			curdepth++
			files, er := ioutil.ReadDir(path)
			if er != nil {
				log.Fatal(er)
			}
			for _, v := range files {
				if v.IsDir() {
					rset(curdepth, totdepth, path + "/" + v.Name())
				}
			}
		}
	}
	lvl := 0
	if *recursive { lvl = 2}
	rset(0,lvl,".")

	// And now wait... ./watch go install
	select {}

	log.Println("I am dead now...")
}

func events() {
	for e := range win.EventChan() { 
		switch e.C2 {
		case 'x', 'X': // execute
			if string(e.Text) == "Get" {
				select {
				case needrun <- true:
				default:
				}
				continue
			}
			if string(e.Text) == "Del" {
				win.Ctl("delete")
				// We should also stop watching
				os.Exit(0)
			}
		}
		win.WriteEvent(e)
	}
	os.Exit(0)
}

var run struct {
	sync.Mutex
	id int
}

func runner() {
	var lastcmd *exec.Cmd
	for _ = range needrun {
		run.Lock()
		run.id++
		id := run.id
		run.Unlock()
		if lastcmd != nil {
			lastcmd.Process.Kill()
		}
		lastcmd = nil
		cmd := exec.Command(args[0], args[1:]...)
		r, w, err := os.Pipe()
		if err != nil {
			log.Fatal(err)
		}
		win.Addr(",")
		win.Write("data", nil)
		win.Ctl("clean")
		win.Fprintf("body", "$ %s\n", strings.Join(args, " "))
		cmd.Stdout = w
		cmd.Stderr = w
		if err := cmd.Start(); err != nil {
			r.Close()
			w.Close()
			win.Fprintf("body", "%s: %s\n", strings.Join(args, " "), err)
			continue
		}
		lastcmd = cmd
		w.Close()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := r.Read(buf)
				if err != nil {
					break
				}
				run.Lock()
				if id == run.id {
					win.Write("body", buf[:n])
				}
				run.Unlock()
			}
			if err := cmd.Wait(); err != nil {
				run.Lock()
				if id == run.id {
					win.Fprintf("body", "%s: %s\n", strings.Join(args, " "), err)
				}
				run.Unlock()
			}
			win.Fprintf("body", "$\n")
			win.Fprintf("addr", "#0")
			win.Ctl("dot=addr")
			win.Ctl("show")
			win.Ctl("clean")
		}()
	}
}
