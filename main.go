package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	"github.com/gammazero/workerpool"
)

type IntListFlag []int
type StringListFlag []string

var (
	copyExtras = flag.Bool("e", false, "Copy extra files after splitting flac")
	outputDir  = flag.String("o", ".", "Output directory")
	quiet      = flag.Bool("q", false, "Only print errors")
	dryRun     = flag.Bool("d", false, "Dry run")
	jsonDump   = flag.Bool("j", false, "Dump all inputs as json")
	format     = flag.String("f", "flac", `Output format. Example: "-f ogg". Any format that ffmpeg supports can be used`)
	trackArgs  IntListFlag
	ffmpegArgs StringListFlag
	nameTmpl   *template.Template
)

func main() {
	flag.Var(&ffmpegArgs, "arg-ffmpeg", `Add an argument to ffmpeg. Example: "-arg-ffmpeg -qscale:a --arg-ffmpeg 2"`)
	flag.Var(&trackArgs, "t", `Extract specific track(s). Example: "-t 1 -t 2"`)
	nameTmplV := flag.String(
		"n",
		`{{.Input.Artist | Elem -}} / {{- with .Input.Date}}{{.}} - {{end}}{{with .Input.Title}}{{. | Elem}}{{else}}Unknown Album{{end -}} / {{- printf .Input.TrackNumberFmt .Track.Number}} - {{.Track.Title | Elem}}`,
		"File naming template",
	)
	help := flag.Bool("h", false, "Show command usage")
	flag.Parse()

	if *help {
		fmt.Fprintf(os.Stderr, "Usage: unflac [OPTION] ... [INPUT] ...\n\n")
		fmt.Fprintf(os.Stderr, "INPUT can be either a directory or a CUE sheet file.\n")
		fmt.Fprintf(os.Stderr, "If no inputs where specified, a current directory is used.\n\n")
		flag.PrintDefaults()
		os.Exit(0)
	}

	var err error
	nameTmpl = template.New("-n").Funcs(template.FuncMap{"Elem": pathReplaceChars})
	if nameTmpl, err = nameTmpl.Parse(*nameTmplV); err != nil {
		log.Fatal(err)
	}

	args := flag.Args()
	if len(args) == 0 {
		args = []string{"."}
	}
	var inputs []*Input
	for _, path := range args {
		if fi, err := os.Stat(path); err != nil {
			log.Fatalf("%s: %s", path, err)
		} else if fi.IsDir() {
			inputs = append(inputs, scanDir(path)...)
		} else if strings.ToLower(filepath.Ext(path)) != ".cue" {
			log.Fatalf("%s: only dirs and CUE sheets are supported as inputs", path)
		} else if in, err := NewInput(path); err != nil {
			log.Fatalf("%s: %s", path, err)
		} else {
			inputs = append(inputs, in)
		}
	}
	if len(inputs) == 0 {
		log.Fatal("no input found")
	}

	wp := workerpool.New(runtime.NumCPU())
	firstErr := make(chan error)
	go func() {
		log.Fatalf("%s", <-firstErr)
	}()

	for _, in := range inputs {
		if !*dryRun {
			if err := in.Split(wp, firstErr); err != nil {
				log.Fatalf("%s: %s", in.Path, err)
			}
		}
	}
	wp.StopWait()

	if *jsonDump {
		json.NewEncoder(os.Stdout).Encode(inputs)
	}
}

func (l *IntListFlag) String() string {
	return fmt.Sprintf("%+v", *l)
}

func (l *IntListFlag) Set(s string) (err error) {
	var i int
	if i, err = strconv.Atoi(s); err == nil {
		*l = append(*l, i)
	}
	return
}

func (l *IntListFlag) Has(i int) bool {
	for _, x := range *l {
		if x == i {
			return true
		}
	}
	return false
}

func (l *StringListFlag) String() string {
	return strings.Join(*l, " ")
}

func (l *StringListFlag) Set(s string) error {
	*l = append(*l, s)
	return nil
}

func scanDir(path string) (results []*Input) {
	var files *os.File
	var fileInfos []os.FileInfo
	var err error
	if files, err = os.Open(path); err == nil {
		if fileInfos, err = files.Readdir(0); err == nil {
			for _, fileItem := range fileInfos {
				name := fileItem.Name()
				filePath := filepath.Join(path, name)
				if fileItem.IsDir() {
					results = append(results, scanDir(filePath)...)
					log.Printf("%s", filePath)
				} else if strings.ToLower(filepath.Ext(name)) == ".cue" {
					var inputFile *Input
					if inputFile, err = NewInput(filePath); err != nil {
						log.Fatalf("%s: %s", filePath, err)
					}
					log.Printf("%s", filePath)
					results = append(results, inputFile)
				} else if *copyExtras {
					if strings.ToLower(filepath.Ext(name)) != ".flac" {
						var inputFile *Input
						if inputFile, err = NewInput(filePath); err != nil {
							log.Fatalf("%s: %s", filePath, err)
						}
						results = append(results, inputFile)
						filepath.Dir(name)
						log.Printf("%s", filePath)
						continue
					}
				}
			}
		}
	}

	if err != nil {
		log.Fatal("%s: %s", path, err)
	}

	return results
}
