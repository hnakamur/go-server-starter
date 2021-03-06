package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/lestrrat/go-server-starter"
	"github.com/lestrrat/go-server-starter/logger"
	daemon "github.com/lomik/go-daemon"
)

const version = "0.0.2"

type options struct {
	OptArgs                []string
	OptCommand             string
	OptDir                 string   `long:"dir" arg:"path" description:"working directory, start_server do chdir to before exec (optional)"`
	OptInterval            int      `long:"interval" arg:"seconds" description:"minimum interval (in seconds) to respawn the server program (default: 1)"`
	OptPorts               []string `long:"port" arg:"(port|host:port)" description:"TCP port to listen to (if omitted, will not bind to any ports)"`
	OptPaths               []string `long:"path" arg:"path" description:"path at where to listen using unix socket (optional)"`
	OptSignalOnHUP         string   `long:"signal-on-hup" arg:"Signal" description:"name of the signal to be sent to the server process when start_server\nreceives a SIGHUP (default: SIGTERM). If you use this option, be sure to\nalso use '--signal-on-term' below."`
	OptSignalOnTERM        string   `long:"signal-on-term" arg:"Signal" description:"name of the signal to be sent to the server process when start_server\nreceives a SIGTERM (default: SIGTERM)"`
	OptPidFile             string   `long:"pid-file" arg:"filename" description:"if set, writes the process id of the start_server process to the file"`
	OptStatusFile          string   `long:"status-file" arg:"filename" description:"if set, writes the status of the server process(es) to the file"`
	OptEnvdir              string   `long:"envdir" arg:"Envdir" description:"directory that contains environment variables to the server processes.\nIt is intended for use with \"envdir\" in \"daemontools\". This can be\noverwritten by environment variable \"ENVDIR\"."`
	OptEnableAutoRestart   bool     `long:"enable-auto-restart" description:"enables automatic restart by time. This can be overwritten by\nenvironment variable \"ENABLE_AUTO_RESTART\"." note:"unimplemented"`
	OptAutoRestartInterval int      `long:"auto-restart-interval" arg:"seconds" description:"automatic restart interval (default 360). It is used with\n\"--enable-auto-restart\" option. This can be overwritten by environment\nvariable \"AUTO_RESTART_INTERVAL\"." note:"unimplemented"`
	OptKillOldDelay        int      `long:"kill-old-delay" arg:"seconds" description:"time to suspend to send a signal to the old worker. The default value is\n5 when \"--enable-auto-restart\" is set, 0 otherwise. This can be\noverwritten by environment variable \"KILL_OLD_DELAY\"."`
	OptRestart             bool     `long:"restart" description:"this is a wrapper command that reads the pid of the start_server process\nfrom --pid-file, sends SIGHUP to the process and waits until the\nserver(s) of the older generation(s) die by monitoring the contents of\nthe --status-file" note:"unimplemented"`
	OptHelp                bool     `long:"help" description:"prints this help"`
	OptVersion             bool     `long:"version" description:"prints the version number"`
	OptDaemon              bool     `long:"daemon" description:"if set, run start_server as a daemon"`
	OptSyslog              bool     `long:"syslog" description:"if set, prints log to syslog instead of stderr"`
	OptSyslogPriority      string   `long:"syslog-priority" arg:"priority" description:"syslog priority. Specify one severity with one or more facilities\n(default: INFO,USER).\nPossible values are those on https://golang.org/pkg/log/syslog/#Priority\nwithout \"LOG_\" prefix."`
	logger                 logger.Logger
}

func (o options) Args() []string          { return o.OptArgs }
func (o options) Command() string         { return o.OptCommand }
func (o options) Dir() string             { return o.OptDir }
func (o options) Interval() time.Duration { return time.Duration(o.OptInterval) * time.Second }
func (o options) PidFile() string         { return o.OptPidFile }
func (o options) Ports() []string         { return o.OptPorts }
func (o options) Paths() []string         { return o.OptPaths }
func (o options) SignalOnHUP() os.Signal  { return starter.SigFromName(o.OptSignalOnHUP) }
func (o options) SignalOnTERM() os.Signal { return starter.SigFromName(o.OptSignalOnTERM) }
func (o options) StatusFile() string      { return o.OptStatusFile }
func (o options) Logger() logger.Logger   { return o.logger }

func showHelp() {
	// The ONLY reason we're not using go-flags' help option is
	// because I wanted to tweak the format just a bit... but
	// there wasn't an easy way to do so
	os.Stderr.WriteString(`
Usage:
      start_server [options] -- server-prog server-arg1 server-arg2 ...

      # start Plack using Starlet listening at TCP port 8000
      start_server --port=8000 -- plackup -s Starlet --max-workers=100 index.psgi

Options:
`)

	t := reflect.TypeOf(options{})

	// This weird indexing stuff is done purely to keep ourselves
	// compatible with the original start_server program
	// (This is the order that the help is displayed in)
	names := []string{
		"OptPorts",
		"OptPaths",
		"OptDir",
		"OptInterval",
		"OptSignalOnHUP",
		"OptSignalOnTERM",
		"OptPidFile",
		"OptStatusFile",
		"OptEnvdir",
		"OptEnableAutoRestart",
		"OptAutoRestartInterval",
		"OptKillOldDelay",
		"OptRestart",
		"OptHelp",
		"OptVersion",
		"OptDaemon",
		"OptSyslog",
		"OptSyslogPriority",
	}

	for _, name := range names {
		f, ok := t.FieldByName(name)
		if !ok {
			continue
		}

		tag := f.Tag
		if tag == "" {
			continue
		}
		if s := tag.Get("long"); s != "" {
			fmt.Fprintf(os.Stderr, "  --%s", s)
			if a := tag.Get("arg"); a != "" {
				fmt.Fprintf(os.Stderr, "=%s", a)
			}
			if tag.Get("note") == "unimplemented" {
				fmt.Fprintf(os.Stderr, " (UNIMPLEMENTED)")
			}
			fmt.Fprintf(os.Stderr, ":\n")
		}
		for _, l := range strings.Split(tag.Get("description"), "\n") {
			fmt.Fprintf(os.Stderr, "    %s\n", l)
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
}

func childMain(args []string, opts *options) (st int) {
	opts.OptCommand = args[0]
	if len(args) > 1 {
		opts.OptArgs = args[1:]
	}

	if opts.OptEnvdir != "" {
		os.Setenv("ENVDIR", opts.OptEnvdir)
	}

	s, err := starter.NewStarter(opts)
	if err != nil {
		opts.logger.Printf("error: %s", err)
		return 1
	}
	s.Run()
	return 0
}

func main() {
	opts := &options{
		OptInterval:       -1,
		OptSyslogPriority: "INFO,USER",
	}
	p := flags.NewParser(opts, flags.PrintErrors|flags.PassDoubleDash)
	args, err := p.Parse()
	if err != nil || opts.OptHelp {
		showHelp()
		os.Exit(1)
	}

	if opts.OptVersion {
		fmt.Printf("%s\n", version)
		os.Exit(0)
	}

	if opts.OptInterval < 0 {
		opts.OptInterval = 1
	}

	if opts.OptSyslog {
		l, err := logger.NewSyslog(opts.OptSyslogPriority)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			os.Exit(1)
		}
		opts.logger = l
	} else {
		opts.logger = logger.NewStderr()
	}

	if len(args) == 0 {
		opts.logger.Printf("server program not specified")
		os.Exit(1)
	}

	if opts.OptDaemon {
		ctx := new(daemon.Context)
		child, err := ctx.Reborn()
		if err != nil {
			opts.logger.Printf("error: %s", err)
			os.Exit(1)
		}
		if child != nil {
			os.Exit(0)
		} else {
			st := childMain(args, opts)
			ctx.Release()
			os.Exit(st)
		}
	} else {
		os.Exit(childMain(args, opts))
	}
}
