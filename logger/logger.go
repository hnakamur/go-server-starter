package logger

import (
	"errors"
	"log"
	"log/syslog"
	"os"
	"strings"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

func NewStderr() *log.Logger {
	return log.New(os.Stderr, "", 0)
}

func NewSyslog(priority string) (*log.Logger, error) {
	p, err := parsePriority(priority)
	if err != nil {
		return nil, err
	}
	return syslog.NewLogger(p, 0)
}

var ErrMultipleLevels = errors.New("cannot specify multiple levels")

func parsePriority(priority string) (syslog.Priority, error) {
	p := syslog.Priority(0)
	seenLevel := false
	for _, term := range strings.Split(strings.ToUpper(priority), ",") {
		switch strings.TrimSpace(term) {
		case "EMERG":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_EMERG
				seenLevel = true
			}
		case "ALERT":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_ALERT
				seenLevel = true
			}
		case "CRIT":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_CRIT
				seenLevel = true
			}
		case "ERR":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_ERR
				seenLevel = true
			}
		case "WARNING":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_WARNING
				seenLevel = true
			}
		case "NOTICE":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_NOTICE
				seenLevel = true
			}
			return syslog.LOG_NOTICE, nil
		case "INFO":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_INFO
				seenLevel = true
			}
		case "DEBUG":
			if seenLevel {
				return 0, ErrMultipleLevels
			} else {
				p |= syslog.LOG_DEBUG
				seenLevel = true
			}
		case "KERN":
			p |= syslog.LOG_KERN
		case "USER":
			p |= syslog.LOG_USER
		case "MAIL":
			p |= syslog.LOG_MAIL
		case "DAEMON":
			p |= syslog.LOG_DAEMON
		case "AUTH":
			p |= syslog.LOG_AUTH
		case "SYSLOG":
			p |= syslog.LOG_SYSLOG
		case "LPR":
			p |= syslog.LOG_LPR
		case "NEWS":
			p |= syslog.LOG_NEWS
		case "UUCP":
			p |= syslog.LOG_UUCP
		case "CRON":
			p |= syslog.LOG_CRON
		case "AUTHPRIV":
			p |= syslog.LOG_AUTHPRIV
		case "FTP":
			p |= syslog.LOG_FTP
		case "LOCAL0":
			p |= syslog.LOG_LOCAL0
		case "LOCAL1":
			p |= syslog.LOG_LOCAL1
		case "LOCAL2":
			p |= syslog.LOG_LOCAL2
		case "LOCAL3":
			p |= syslog.LOG_LOCAL3
		case "LOCAL4":
			p |= syslog.LOG_LOCAL4
		case "LOCAL5":
			p |= syslog.LOG_LOCAL5
		case "LOCAL6":
			p |= syslog.LOG_LOCAL6
		case "LOCAL7":
			p |= syslog.LOG_LOCAL7
		default:
			return 0, errors.New("invalid priority")
		}
	}
	return p, nil
}
