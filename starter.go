package starter

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lestrrat/go-server-starter/logger"
)

var niceSigNames map[syscall.Signal]string
var niceNameToSigs map[string]syscall.Signal
var successStatus syscall.WaitStatus
var failureStatus syscall.WaitStatus

func makeNiceSigNamesCommon() map[syscall.Signal]string {
	return map[syscall.Signal]string{
		syscall.SIGABRT: "ABRT",
		syscall.SIGALRM: "ALRM",
		syscall.SIGBUS:  "BUS",
		// syscall.SIGEMT:  "EMT",
		syscall.SIGFPE: "FPE",
		syscall.SIGHUP: "HUP",
		syscall.SIGILL: "ILL",
		// syscall.SIGINFO: "INFO",
		syscall.SIGINT: "INT",
		// syscall.SIGIOT:    "IOT",
		syscall.SIGKILL: "KILL",
		syscall.SIGPIPE: "PIPE",
		syscall.SIGQUIT: "QUIT",
		syscall.SIGSEGV: "SEGV",
		syscall.SIGTERM: "TERM",
		syscall.SIGTRAP: "TRAP",
	}
}

func makeNiceSigNames() map[syscall.Signal]string {
	return addPlatformDependentNiceSigNames(makeNiceSigNamesCommon())
}

func init() {
	niceSigNames = makeNiceSigNames()
	niceNameToSigs := make(map[string]syscall.Signal)
	for sig, name := range niceSigNames {
		niceNameToSigs[name] = sig
	}
}

type listener struct {
	listener net.Listener
	spec     string // path or port spec
}

type Config interface {
	Args() []string
	Command() string
	Dir() string             // Dirctory to chdir to before executing the command
	Interval() time.Duration // Time between checks for liveness
	PidFile() string
	Ports() []string         // Ports to bind to (addr:port or port, so it's a string)
	Paths() []string         // Paths (UNIX domain socket) to bind to
	SignalOnHUP() os.Signal  // Signal to send when HUP is received
	SignalOnTERM() os.Signal // Signal to send when TERM is received
	StatusFile() string
	Logger() logger.Logger
}

type Starter struct {
	interval     time.Duration
	signalOnHUP  os.Signal
	signalOnTERM os.Signal
	// you can't set this in go:	backlog
	statusFile string
	pidFile    string
	dir        string
	ports      []string
	paths      []string
	listeners  []listener
	generation int
	command    string
	args       []string
	logger     logger.Logger
}

// NewStarter creates a new Starter object. Config parameter may NOT be
// nil, as `Ports` and/or `Paths`, and `Command` are required
func NewStarter(c Config) (*Starter, error) {
	if c == nil {
		return nil, fmt.Errorf("config argument must be non-nil")
	}

	var signalOnHUP os.Signal = syscall.SIGTERM
	var signalOnTERM os.Signal = syscall.SIGTERM
	if s := c.SignalOnHUP(); s != nil {
		signalOnHUP = s
	}
	if s := c.SignalOnTERM(); s != nil {
		signalOnTERM = s
	}

	if c.Command() == "" {
		return nil, fmt.Errorf("argument Command must be specified")
	}
	if _, err := exec.LookPath(c.Command()); err != nil {
		return nil, err
	}

	s := &Starter{
		args:         c.Args(),
		command:      c.Command(),
		dir:          c.Dir(),
		interval:     c.Interval(),
		listeners:    make([]listener, 0, len(c.Ports())+len(c.Paths())),
		pidFile:      c.PidFile(),
		ports:        c.Ports(),
		paths:        c.Paths(),
		signalOnHUP:  signalOnHUP,
		signalOnTERM: signalOnTERM,
		statusFile:   c.StatusFile(),
		logger:       c.Logger(),
	}

	return s, nil

}

func (s Starter) Stop() {
	p, _ := os.FindProcess(os.Getpid())
	p.Signal(syscall.SIGTERM)
}

func grabExitStatus(st processState) syscall.WaitStatus {
	// Note: POSSIBLY non portable. seems to work on Unix/Windows
	// When/if this blows up, we will look for a cure
	exitSt, ok := st.Sys().(syscall.WaitStatus)
	if !ok {
		fmt.Fprintf(os.Stderr, "Oh no, you are running on a platform where ProcessState.Sys().(syscall.WaitStatus) doesn't work! We're doomed! Temporarily setting status to 255. Please contact the author about this\n")
		exitSt = failureStatus
	}
	return exitSt
}

type processState interface {
	Pid() int
	Sys() interface{}
}
type dummyProcessState struct {
	pid    int
	status syscall.WaitStatus
}

func (d dummyProcessState) Pid() int {
	return d.pid
}

func (d dummyProcessState) Sys() interface{} {
	return d.status
}

func signame(s os.Signal) string {
	if ss, ok := s.(syscall.Signal); ok {
		return niceSigNames[ss]
	}
	return "UNKNOWN"
}

func SigFromName(n string) os.Signal {
	if sig, ok := niceNameToSigs[n]; ok {
		return sig
	}
	return nil
}

func setEnv() error {
	if os.Getenv("ENVDIR") == "" {
		return nil
	}

	m, err := reloadEnv()
	if err != nil && err != errNoEnv {
		// do something
		return fmt.Errorf("failed to load from envdir: %s", err)
	}

	for k, v := range m {
		os.Setenv(k, v)
	}
	return nil
}

func parsePortSpec(addr string) (string, int, error) {
	i := strings.IndexByte(addr, ':')
	portPart := ""
	if i < 0 {
		portPart = addr
		addr = ""
	} else {
		portPart = addr[i+1:]
		addr = addr[:i]
	}

	port, err := strconv.ParseInt(portPart, 10, 64)
	if err != nil {
		return "", -1, err
	}

	return addr, int(port), nil
}

func (s *Starter) Run() error {
	defer s.Teardown()

	if s.pidFile != "" {
		f, err := os.OpenFile(s.pidFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}

		fmt.Fprintf(f, "%d", os.Getpid())
		f.Close()
	}

	for _, addr := range s.ports {
		var l net.Listener

		host, port, err := parsePortSpec(addr)
		if err != nil {
			s.logger.Printf("failed to parse addr spec '%s': %s", addr, err)
			return err
		}

		hostport := fmt.Sprintf("%s:%d", host, port)
		l, err = net.Listen("tcp4", hostport)
		if err != nil {
			s.logger.Printf("failed to listen to %s:%s", hostport, err)
			return err
		}

		spec := ""
		if host == "" {
			spec = fmt.Sprintf("%d", port)
		} else {
			spec = fmt.Sprintf("%s:%d", host, port)
		}
		s.listeners = append(s.listeners, listener{listener: l, spec: spec})
	}

	for _, path := range s.paths {
		var l net.Listener
		if fl, err := os.Lstat(path); err == nil && fl.Mode()&os.ModeSocket == os.ModeSocket {
			s.logger.Printf("removing existing socket file:%s", path)
			err = os.Remove(path)
			if err != nil {
				s.logger.Printf("failed to remove existing socket file:%s:%s", path, err)
				return err
			}
		}
		_ = os.Remove(path)
		l, err := net.Listen("unix", path)
		if err != nil {
			s.logger.Printf("failed to listen file:%s:%s", path, err)
			return err
		}
		s.listeners = append(s.listeners, listener{listener: l, spec: path})
	}

	s.generation = 0
	os.Setenv("SERVER_STARTER_GENERATION", fmt.Sprintf("%d", s.generation))

	// XXX Not portable
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// Okay, ready to launch the program now...
	err := setEnv()
	if err != nil {
		s.logger.Printf("%s", err)
	}
	workerCh := make(chan processState)
	p := s.StartWorker(sigCh, workerCh)
	oldWorkers := make(map[int]int)
	var sigReceived os.Signal
	var sigToSend os.Signal

	statusCh := make(chan map[int]int)
	go func(fn string, ch chan map[int]int) {
		for wmap := range ch {
			if fn == "" {
				continue
			}

			f, err := os.OpenFile(fn, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				continue
			}

			for gen, pid := range wmap {
				fmt.Fprintf(f, "%d:%d\n", gen, pid)
			}

			f.Close()
		}
	}(s.statusFile, statusCh)

	defer func() {
		if p != nil {
			oldWorkers[p.Pid] = s.generation
		}

		size := len(oldWorkers)
		var b []byte
		i := 0
		for pid := range oldWorkers {
			i++
			b = strconv.AppendInt(b, int64(pid), 10)
			if i < size {
				b = append(b, ',')
			}
		}
		s.logger.Printf("received %s, sending %s to all workers:%s",
			signame(sigReceived),
			signame(sigToSend),
			string(b),
		)

		for pid := range oldWorkers {
			worker, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			worker.Signal(sigToSend)
		}

		for len(oldWorkers) > 0 {
			st := <-workerCh
			s.logger.Printf("worker %d died, status:%d", st.Pid(), grabExitStatus(st))
			delete(oldWorkers, st.Pid())
		}
		s.logger.Printf("exiting")
	}()

	//	var lastRestartTime time.Time
	for { // outer loop
		err = setEnv()
		if err != nil {
			s.logger.Printf("%s", err)
		}

		// Just wait for the worker to exit, or for us to receive a signal
		for {
			// restart = 2: force restart
			// restart = 1 and no workers: force restart
			// restart = 0: no restart
			restart := 0

			select {
			case st := <-workerCh:
				// oops, the worker exited? check for its pid
				if p.Pid == st.Pid() { // current worker
					exitSt := grabExitStatus(st)
					s.logger.Printf("worker %d died unexpectedly with status %d, restarting", p.Pid, exitSt)
					p = s.StartWorker(sigCh, workerCh)
					// lastRestartTime = time.Now()
				} else {
					exitSt := grabExitStatus(st)
					s.logger.Printf("old worker %d died, status:%d", st.Pid(), exitSt)
					delete(oldWorkers, st.Pid())
				}
			case sigReceived = <-sigCh:
				// Temporary fix
				switch sigReceived {
				case syscall.SIGHUP:
					// When we receive a HUP signal, we need to spawn a new worker
					s.logger.Printf("received HUP (num_old_workers=TODO)")
					restart = 1
					sigToSend = s.signalOnHUP
				case syscall.SIGTERM:
					sigToSend = s.signalOnTERM
					return nil
				default:
					sigToSend = syscall.SIGTERM
					return nil
				}
			}

			if restart > 1 || restart > 0 && len(oldWorkers) == 0 {
				s.logger.Printf("spawning a new worker (num_old_workers=TODO)")
				oldWorkers[p.Pid] = s.generation
				p = s.StartWorker(sigCh, workerCh)
				size := len(oldWorkers)
				if size == 0 {
					s.logger.Printf("new worker is now running, sending %s to old workers:none", signame(sigToSend))
				} else {
					i := 0
					var b []byte
					for pid := range oldWorkers {
						i++
						b = strconv.AppendInt(b, int64(pid), 10)
						if i < size {
							b = append(b, ',')
						}
					}
					s.logger.Printf("new worker is now running, sending %s to old workers:%s", signame(sigToSend), string(b))

					killOldDelay := getKillOldDelay()
					s.logger.Printf("sleep %d secs", int(killOldDelay/time.Second))
					if killOldDelay > 0 {
						time.Sleep(killOldDelay)
					}

					s.logger.Printf("killing old workers")

					for pid := range oldWorkers {
						worker, err := os.FindProcess(pid)
						if err != nil {
							continue
						}
						worker.Signal(s.signalOnHUP)
					}
				}
			}
		}
	}

	return nil
}

func getKillOldDelay() time.Duration {
	// Ignore errors.
	delay, _ := strconv.ParseInt(os.Getenv("KILL_OLD_DELAY"), 10, 0)
	autoRestart, _ := strconv.ParseBool(os.Getenv("ENABLE_AUTO_RESTART"))
	if autoRestart && delay == 0 {
		delay = 5
	}

	return time.Duration(delay) * time.Second
}

type WorkerState int

const (
	WorkerStarted WorkerState = iota
	ErrFailedToStart
)

// StartWorker starts the actual command.
func (s *Starter) StartWorker(sigCh chan os.Signal, ch chan processState) *os.Process {
	// Don't give up until we're running.
	for {
		pid := -1
		cmd := exec.Command(s.command, s.args...)
		if s.dir != "" {
			cmd.Dir = s.dir
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// This whole section here basically sets up the env
		// var and the file descriptors that are inherited by the
		// external process
		files := make([]*os.File, len(s.ports)+len(s.paths))
		ports := make([]string, len(s.ports)+len(s.paths))
		for i, l := range s.listeners {
			// file descriptor numbers in ExtraFiles turn out to be
			// index + 3, so we can just hard code it
			var f *os.File
			var err error
			switch l.listener.(type) {
			case *net.TCPListener:
				f, err = l.listener.(*net.TCPListener).File()
			case *net.UnixListener:
				f, err = l.listener.(*net.UnixListener).File()
			default:
				panic("Unknown listener type")
			}
			if err != nil {
				panic(err)
			}
			defer f.Close()
			ports[i] = fmt.Sprintf("%s=%d", l.spec, i+3)
			files[i] = f
		}
		cmd.ExtraFiles = files

		s.generation++
		os.Setenv("SERVER_STARTER_PORT", strings.Join(ports, ";"))
		os.Setenv("SERVER_STARTER_GENERATION", fmt.Sprintf("%d", s.generation))

		// Now start!
		if err := cmd.Start(); err != nil {
			s.logger.Printf("failed to exec %s: %s", cmd.Path, err)
		} else {
			// Save pid...
			pid = cmd.Process.Pid
			s.logger.Printf("starting new worker %d", pid)

			// Wait for interval before checking if the process is alive
			tch := time.After(s.interval)
			sigs := []os.Signal{}
			for loop := true; loop; {
				select {
				case <-tch:
					// bail out
					loop = false
				case sig := <-sigCh:
					sigs = append(sigs, sig)
				}
			}

			// if received any signals, during the wait, we bail out
			gotSig := false
			if len(sigs) > 0 {
				for _, sig := range sigs {
					// we need to resend these signals so it can be caught in the
					// main routine...
					go func() { sigCh <- sig }()
					if sysSig, ok := sig.(syscall.Signal); ok {
						if sysSig != syscall.SIGHUP {
							gotSig = true
						}
					}
				}
			}

			// Check if we can find a process by its pid
			p, err := os.FindProcess(pid)
			if gotSig || err == nil {
				// No error? We were successful! Make sure we capture
				// the program exiting
				go func() {
					err := cmd.Wait()
					if err != nil {
						ch <- err.(*exec.ExitError).ProcessState
					} else {
						ch <- &dummyProcessState{pid: pid, status: successStatus}
					}
				}()
				// Bail out
				return p
			}

		}
		// If we fall through here, we prematurely exited :/
		// Make sure to wait to release resources
		cmd.Wait()
		for _, f := range cmd.ExtraFiles {
			f.Close()
		}

		s.logger.Printf("new worker %d seems to have failed to start", pid)
	}

	// never reached
	return nil
}

func (s *Starter) Teardown() error {
	if s.pidFile != "" {
		os.Remove(s.pidFile)
	}

	if s.statusFile != "" {
		os.Remove(s.statusFile)
	}

	for _, l := range s.listeners {
		l.listener.Close()
	}

	return nil
}
