package main

import (
	"github.com/asobrien/hookserve/hookserve"
	"github.com/asobrien/nomaster/scotch"
	"github.com/spf13/viper"
	"gopkg.in/urfave/cli.v1"

	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"time"
)

var (
	Version string = "0.1.1"
	config         = viper.New()
	Trace   *log.Logger
	Info    *log.Logger
	Warning *log.Logger
	Error   *log.Logger
)

var cliParam = new(cliFlag)

// cliFlag allows for cli <--> viper conversion
type cliFlag struct {
	Port        int
	port        int
	token       string
	secret      string
	logfile     string
	path        string
	healthcheck string
	debug       bool
}

// getField returns clfFlag field value via string
// this allows for cli --> viper conversion in a loop
func (c *cliFlag) getField(field string) interface{} {
	r := reflect.ValueOf(c)
	f := reflect.Indirect(r).FieldByName(field)

	switch f.Type().Name() {
	default:
		panic(fmt.Errorf("Unknown type: %T\n", f))
	case "int":
		return f.Int()
	case "string":
		return f.String()
	case "bool":
		return f.Bool()
	}
	return nil
}

// PullRequest Event Handler
type PullRequest struct {
	hookserve.Event
	Domain string
}

// Path returns the URL of the base GitHub repo
func (e *PullRequest) Path() string {
	return e.Domain + "/" + "repos/" + e.BaseOwner + "/" + e.BaseRepo
}

// Make comment on a PR, return status code and body
func (e *PullRequest) Comment(cmt string) {
	// Create a comment: POST /repos/:owner/:repo/issues/:number/comments
	path := "https://" + e.Path() + "/" + "issues" + "/" + strconv.Itoa(e.IssueID) + "/" + "comments"

	// generate request
	dataStr := fmt.Sprintf("{\"body\":\"%s\"}", cmt)

	req, err := http.NewRequest("POST", path, bytes.NewBufferString(dataStr))
	req.Header.Set("Authorization", "token "+config.GetString("token"))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	Trace.Printf("POST %v - Commenting PR #%v\n", path, e.IssueID)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		Error.Println(err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		Warning.Printf("POST %v - returned error code %v: %v\n",
			path,
			resp.Status,
			string(body))
	}
}

func (e *PullRequest) SetState(state string) {
	// Update a PR: PATCH /repos/:owner/:repo/pulls/:number
	path := "https://" + e.Path() + "/" + "pulls" + "/" + strconv.Itoa(e.IssueID)

	dataStr := fmt.Sprintf("{\"state\":\"%s\"}", state)

	req, err := http.NewRequest("PATCH", path, bytes.NewBufferString(dataStr))
	req.Header.Set("Authorization", "token "+config.GetString("token"))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	Trace.Printf("PATCH %v - Closing PR #%v\n", path, e.IssueID)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		Error.Println(err)
	}
	defer resp.Body.Close()
}

// Initializes loggers, to debug or not?
func initLoggers(debug bool) error {
	var err error
	var logFile = os.Stdout
	var traceWriter = ioutil.Discard
	var infoWriter = io.MultiWriter(os.Stdout)
	var warningWriter = io.MultiWriter(os.Stdout)
	var errorWriter = io.MultiWriter(os.Stderr)

	// Create logFile if specified
	if config.IsSet("logfile") && config.GetString("logfile") != "" {
		logFile, err = os.OpenFile(config.GetString("logfile"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
		if err != nil {
			// no logger is setup yet, so write to Stderr
			fmt.Fprintln(os.Stderr, "Failed to open log file: ", err)
			os.Exit(1)
		}
	}

	// debug always writes to Stdout/Stderr
	if debug && logFile != os.Stdout {
		traceWriter = io.MultiWriter(os.Stdout, logFile)
		infoWriter = traceWriter
		warningWriter = traceWriter
		errorWriter = io.MultiWriter(os.Stderr, logFile)
	} else if debug {
		traceWriter = infoWriter
	}

	// Write to log not Stdout/Stderr if logFile is specified
	if logFile != os.Stdout {
		infoWriter := io.MultiWriter(logFile)
		warningWriter = infoWriter
		errorWriter = infoWriter
	}

	// Enable loggers
	Trace = log.New(traceWriter,
		"[TRACE  ] ",
		log.LstdFlags)

	Info = log.New(infoWriter,
		"[INFO   ] ",
		log.LstdFlags)

	Warning = log.New(warningWriter,
		"[WARNING] ",
		log.LstdFlags)

	Error = log.New(errorWriter,
		"[ERROR  ] ",
		log.LstdFlags)

	return nil
}

//Run the server and respond to PRs
// We need viper to get config data
func serve() {
	server := hookserve.NewServer()
	server.Port = config.GetInt("port")
	server.Path = config.GetString("path")
	server.Ping = config.GetString("healthcheck")
	server.Secret = config.GetString("secret")
	server.GoListenAndServe()

	for {
		select {
		case hook := <-server.Events:

			var comment string

			if hook.Type != "pull_request" {
				Warning.Printf("Forbidden hook type: %v", hook.Type)
				return
			}

			event := PullRequest{
				Event:  hook,
				Domain: "api.github.com",
			}

			if config.IsSet("comment") {
				comment = config.GetString("comment")
			} else {
				comment = fmt.Sprintf("A bottle of %v would be appropriate here because "+
					"we don't make pull requests against master!",
					scotch.Scotches[rand.Intn(len(scotch.Scotches))])
			}

			// comment and close PR
			if (event.Action == "opened" || event.Action == "reopened") &&
				event.BaseBranch == "master" {
				Info.Printf("[%v/%v] PR #%v closed, %v => %v\n",
					event.BaseOwner,
					event.BaseRepo,
					event.IssueID,
					event.Branch,
					event.BaseBranch)
				// close and comment, async
				go func() {
					event.SetState("closed")
					event.Comment(comment)
				}()
			} else {
				Trace.Printf("[%v/%v] PR #%v %v\n",
					event.BaseOwner,
					event.BaseRepo,
					event.IssueID,
					event.Action)
			}

		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

}

func runApp(c *cli.Context) error {

	// Set file name & search paths
	config.SetConfigName("config")
	config.AddConfigPath("/etc/nomaster/")
	config.AddConfigPath("$HOME/.nomaster")

	// Exit if config has errors, outside of file not found
	err := config.ReadInConfig()
	if err != nil && err != viper.UnsupportedConfigError("") {
		fmt.Fprintln(os.Stderr, "Error reading configuration file: ", err)
		os.Exit(1)
	}

	// Set cli flags if not set and viper.IsSet(flag)
	// this gets rid of globals and we just pass around cli Context
	// Set cli overrides & defaults into viper
	for _, flag := range c.GlobalFlagNames() {
		if !config.IsSet(flag) {
			config.Set(flag, cliParam.getField(flag)) // set default (cli option or default)
		}
		if c.IsSet(flag) {
			config.Set(flag, cliParam.getField(flag)) // explicitly set option (cli flag given)
		}
	}

	initLoggers(config.GetBool("debug"))

	if config.ConfigFileUsed() != "" {
		Trace.Println("Configuration file: " + config.ConfigFileUsed())
	} else {
		Trace.Println("No configuration file found")
	}

	// run the server
	Info.Printf("Running nomaster on port %v ...\n", config.GetInt("port"))
	Trace.Println("Application endpoint: " + config.GetString("path"))
	Trace.Println("Healthcheck endpoint: " + config.GetString("healthcheck"))

	serve()

	return nil
}

func main() {
	app := cli.NewApp()
	app.Action = runApp
	app.Name = "nomaster"
	app.Usage = `A small Github webhook server to shutdown pull
		requests against master.`
	app.UsageText = "nomaster [options]"
	app.Version = Version
	app.Compiled = time.Now()
	app.Authors = []cli.Author{
		cli.Author{
			Name: "Anthony O'Brien",
			// Email: "human@example.com",
		},
	}
	// set cli/config options
	app.Flags = []cli.Flag{
		// port flag
		cli.IntFlag{
			Name:        "port, p",
			Value:       8888,
			Usage:       "sever `PORT`",
			Destination: &cliParam.port,
		},
		// oauth token flag
		cli.StringFlag{
			Name:        "token, t",
			Value:       "",
			Usage:       "Github OAuth `TOKEN`",
			Destination: &cliParam.token,
		},
		// webhook secret signing key
		cli.StringFlag{
			Name:        "secret, s",
			Value:       "",
			Usage:       "Signing `SECRET` key",
			Destination: &cliParam.secret,
		},
		// logfile location
		cli.StringFlag{
			Name:        "logfile, l",
			Usage:       "Path to log `FILE`",
			Destination: &cliParam.logfile,
		},
		// hookserve endpoint
		cli.StringFlag{
			Name:        "path, e",
			Value:       "/",
			Usage:       "Webhook application `ENDPOINT`",
			Destination: &cliParam.path,
		},
		// hookserve endpoint
		cli.StringFlag{
			Name:        "healthcheck, c",
			Value:       "/ping",
			Usage:       "Healthcheck `ENDPOINT`",
			Destination: &cliParam.healthcheck,
		},
		// debug mode
		cli.BoolFlag{
			Name:        "debug, d",
			Usage:       "Enable debug mode",
			Destination: &cliParam.debug,
		},
	}
	app.Run(os.Args)
}
