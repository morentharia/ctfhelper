package utils

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	mr "math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ysmood/gson"
)

// Logger interface
type Logger interface {
	// Same as fmt.Printf
	Println(...interface{})
}

// Log type for Println
type Log func(msg ...interface{})

// Println interface
func (l Log) Println(msg ...interface{}) {
	l(msg...)
}

// LoggerQuiet does nothing
var LoggerQuiet Logger = loggerQuiet{}

type loggerQuiet struct{}

// Println interface
func (l loggerQuiet) Println(...interface{}) {}

// MultiLogger is similar to https://golang.org/pkg/io/#MultiWriter
func MultiLogger(list ...Logger) Log {
	return Log(func(msg ...interface{}) {
		for _, lg := range list {
			lg.Println(msg...)
		}
	})
}

// E if the last arg is error, panic it
func E(args ...interface{}) []interface{} {
	err, ok := args[len(args)-1].(error)
	if ok {
		panic(err)
	}
	return args
}

// S Template render, the params is key-value pairs
func S(tpl string, params ...interface{}) string {
	var out bytes.Buffer

	dict := map[string]interface{}{}
	fnDict := template.FuncMap{}

	l := len(params)
	for i := 0; i < l-1; i += 2 {
		k := params[i].(string)
		v := params[i+1]
		if reflect.TypeOf(v).Kind() == reflect.Func {
			fnDict[k] = v
		} else {
			dict[k] = v
		}
	}

	t := template.Must(template.New("").Funcs(fnDict).Parse(tpl))
	E(t.Execute(&out, dict))

	return out.String()
}

// RandString generate random string with specified string length
func RandString(len int) string {
	b := make([]byte, len)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Mkdir makes dir recursively
func Mkdir(path string) error {
	return os.MkdirAll(path, 0775)
}

// OutputFile auto creates file if not exists, it will try to detect the data type and
// auto output binary, string or json
func OutputFile(p string, data interface{}) error {
	dir := filepath.Dir(p)
	_ = Mkdir(dir)

	var bin []byte

	switch t := data.(type) {
	case []byte:
		bin = t
	case string:
		bin = []byte(t)
	case io.Reader:
		f, _ := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0664)
		_, err := io.Copy(f, t)
		return err
	default:
		bin = MustToJSONBytes(data)
	}

	return ioutil.WriteFile(p, bin, 0664)
}

// ReadString reads file as string
func ReadString(p string) (string, error) {
	bin, err := ioutil.ReadFile(p)
	return string(bin), err
}

// All run all actions concurrently, returns the wait function for all actions.
func All(actions ...func()) func() {
	wg := &sync.WaitGroup{}

	wg.Add(len(actions))

	runner := func(action func()) {
		defer wg.Done()
		action()
	}

	for _, action := range actions {
		go runner(action)
	}

	return wg.Wait
}

// Sleep the goroutine for specified seconds, such as 2.3 seconds
func Sleep(seconds float64) {
	d := time.Duration(seconds * float64(time.Second))
	time.Sleep(d)
}

// Sleeper sleeps the current gouroutine for sometime, returns the reason to wake, if ctx is done release resource
type Sleeper func(context.Context) error

// CountSleeper wake immediately. When counts to the max returns errors.New("max sleep count")
func CountSleeper(max int) Sleeper {
	count := 0
	return func(ctx context.Context) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if count == max {
			return errors.New("max sleep count")
		}
		count++
		return nil
	}
}

// DefaultBackoff algorithm: A(n) = A(n-1) * random[1.9, 2.1)
func DefaultBackoff(interval time.Duration) time.Duration {
	scale := 2 + (mr.Float64()-0.5)*0.2
	return time.Duration(float64(interval) * scale)
}

// BackoffSleeper returns a sleeper that sleeps in a backoff manner every time get called.
// If algorithm is nil, DefaultBackoff will be used.
// Set interval and maxInterval to the same value to make it a constant sleeper.
// If maxInterval is not greater than 0, the sleeper will wake immediately.
func BackoffSleeper(init, maxInterval time.Duration, algorithm func(time.Duration) time.Duration) Sleeper {
	if algorithm == nil {
		algorithm = DefaultBackoff
	}

	return func(ctx context.Context) error {
		// wake immediately
		if maxInterval <= 0 {
			return nil
		}

		var interval time.Duration
		if init < maxInterval {
			interval = algorithm(init)
		} else {
			interval = maxInterval
		}

		t := time.NewTimer(interval)
		defer t.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			init = interval
		}

		return nil
	}
}

// IdleCounter is similar to sync.WaitGroup but it only resolves if no jobs for specified duration.
type IdleCounter struct {
	lock     *sync.Mutex
	job      int
	duration time.Duration
	tmr      *time.Timer
}

// NewIdleCounter ...
func NewIdleCounter(d time.Duration) *IdleCounter {
	tmr := time.NewTimer(time.Hour)
	tmr.Stop()

	return &IdleCounter{
		lock:     &sync.Mutex{},
		duration: d,
		tmr:      tmr,
	}
}

// Add ...
func (de *IdleCounter) Add() {
	de.lock.Lock()
	defer de.lock.Unlock()

	de.tmr.Stop()
	de.job++
}

// Done ...
func (de *IdleCounter) Done() {
	de.lock.Lock()
	defer de.lock.Unlock()

	de.job--
	if de.job == 0 {
		de.tmr.Reset(de.duration)
	}
	if de.job < 0 {
		panic("all jobs are already done")
	}
}

// Wait ...
func (de *IdleCounter) Wait(ctx context.Context) {
	de.lock.Lock()
	if de.job == 0 {
		de.tmr.Reset(de.duration)
	}
	de.lock.Unlock()

	select {
	case <-ctx.Done():
		de.tmr.Stop()
	case <-de.tmr.C:
	}
}

// Retry fn and sleeper until fn returns true or s returns error
func Retry(ctx context.Context, s Sleeper, fn func() (stop bool, err error)) error {
	for {
		stop, err := fn()
		if stop {
			return err
		}
		err = s(ctx)
		if err != nil {
			return err
		}
	}
}

var chPause = make(chan struct{})

// Pause the goroutine forever
func Pause() {
	<-chPause
}

// Dump values for debugging
func Dump(list ...interface{}) string {
	out := []string{}
	for _, el := range list {
		out = append(out, gson.New(el).JSON("", "  "))
	}
	return strings.Join(out, " ")
}

// MustToJSONBytes encode data to json bytes
func MustToJSONBytes(data interface{}) []byte {
	buf := bytes.NewBuffer(nil)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	E(enc.Encode(data))
	b := buf.Bytes()
	return b[:len(b)-1]
}

// MustToJSON encode data to json string
func MustToJSON(data interface{}) string {
	return string(MustToJSONBytes(data))
}

// FileExists checks if file exists, only for file, not for dir
func FileExists(path string) bool {
	info, err := os.Stat(path)

	if err != nil {
		return false
	}

	if info.IsDir() {
		return false
	}

	return true
}

// Exec command
func Exec(name string, args ...string) {
	cmd := exec.Command(name, args...)
	SetCmdStdPipe(cmd)
	E(cmd.Run())
}

// SetCmdStdPipe command
func SetCmdStdPipe(cmd *exec.Cmd) {
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
}

// EscapeGoString not using encoding like base64 or gzip because of they will make git diff every large for small change
func EscapeGoString(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "` + \"`\" + `") + "`"
}
