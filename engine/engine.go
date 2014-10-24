package engine

import (
	"errors"
	"io"
	"log"
	"net"
	"os"

	"github.com/docker/libchan"
	"github.com/docker/libchan/bus"
)

var (
	ErrReceiverDoesNotExist = errors.New("receiver does not exist")
	ErrJobDoesNotExist      = errors.New("job handler does not exist")
	ErrJobAlreadyStarted    = errors.New("job already started")
)

// JobRunner is the description of a process to be submitted and run through libchan
type JobRunner struct {
	job *Job

	client bus.Client
}

// Job is a unit of work to be performed
type Job struct {
	Eng  *Engine
	Name string
	Args []string
	Env  *Env
	Out  libchan.Sender

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// TODO: add support for files
}

func (j *JobRunner) StdinPipe() (io.WriteCloser, error) {
	pr, pw := io.Pipe()
	j.job.Stdin = pr
	return pw, nil
}

func (j *JobRunner) StdoutPipe() (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	j.job.Stdout = pw
	return pr, nil
}

func (j *JobRunner) StderrPipe() (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	j.job.Stderr = pw
	return pr, nil
}

// Run runs the job configured by the runner and returns the result
func (j *JobRunner) Run() error {
	if j.job.Stdin == nil {
		var err error
		j.job.Stdin, err = os.Open(os.DevNull)
		if err != nil {
			return err
		}
	}

	if err := j.client.Message(j.job.Name, j.job); err != nil {
		return err
	}
	return nil
}

type JobHandler func(*Job) error

type Engine struct {
	client bus.Client
}

func NewEngine(client bus.Client) *Engine {
	return &Engine{
		client: client,
	}
}

func NewNetEngine(conn net.Conn, name string) (*Engine, error) {
	client, err := bus.NewNetClient(conn, name)
	if err != nil {
		return nil, err
	}
	return NewEngine(client), nil
}

func NewLocalEngine(b *bus.Bus, name string) (*Engine, error) {
	client, err := bus.NewLocalClient(b, name)
	if err != nil {
		return nil, err
	}
	return NewEngine(client), nil
}

func (eng *Engine) handleReceiver(receiver libchan.Receiver, handler JobHandler) {
	for {
		job := &Job{}
		if err := receiver.Receive(job); err != nil {
			log.Printf("Error receiving job: %s", err)
		}
		if err := handler(job); err != nil {
			log.Printf("Error handling job: %s", err)
		}
		if closer, ok := job.Stdout.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				log.Printf("Error closing stdout: %s", err)
			}
		}
		if closer, ok := job.Stderr.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				log.Printf("Error closing stderr: %s", err)
			}
		}
	}
}

func (eng *Engine) RegisterJob(name string, handler JobHandler) error {
	recv, err := eng.client.Register(name)
	if err != nil {
		return err
	}

	go eng.handleReceiver(recv, handler)

	return nil
}

func (eng *Engine) Job(name string) (*JobRunner, error) {
	return &JobRunner{
		job: &Job{
			Name: name,
			Env:  &Env{},
		},
		client: eng.client,
	}, nil
}

func (job *Job) EnvExists(key string) (value bool) {
	return job.Env.Exists(key)
}

func (job *Job) Getenv(key string) (value string) {
	return job.Env.Get(key)
}

func (job *Job) GetenvBool(key string) (value bool) {
	return job.Env.GetBool(key)
}

func (job *Job) SetenvBool(key string, value bool) {
	job.Env.SetBool(key, value)
}

func (r *JobRunner) SetenvBool(key string, value bool) {
	r.job.SetenvBool(key, value)
}

func (job *Job) GetenvSubEnv(key string) *Env {
	return job.Env.GetSubEnv(key)
}

func (job *Job) SetenvSubEnv(key string, value *Env) error {
	return job.Env.SetSubEnv(key, value)
}

func (job *Job) GetenvInt64(key string) int64 {
	return job.Env.GetInt64(key)
}

func (job *Job) GetenvInt(key string) int {
	return job.Env.GetInt(key)
}

func (job *Job) SetenvInt64(key string, value int64) {
	job.Env.SetInt64(key, value)
}

func (job *Job) SetenvInt(key string, value int) {
	job.Env.SetInt(key, value)
}

// Returns nil if key not found
func (job *Job) GetenvList(key string) []string {
	return job.Env.GetList(key)
}

func (job *Job) GetenvJson(key string, iface interface{}) error {
	return job.Env.GetJson(key, iface)
}

func (job *Job) SetenvJson(key string, value interface{}) error {
	return job.Env.SetJson(key, value)
}

func (job *Job) SetenvList(key string, value []string) error {
	return job.Env.SetJson(key, value)
}

func (job *Job) Setenv(key, value string) {
	job.Env.Set(key, value)
}

// DecodeEnv decodes `src` as a json dictionary, and adds
// each decoded key-value pair to the environment.
//
// If `src` cannot be decoded as a json dictionary, an error
// is returned.
func (job *Job) DecodeEnv(src io.Reader) error {
	return job.Env.Decode(src)
}

func (job *Job) EncodeEnv(dst io.Writer) error {
	return job.Env.Encode(dst)
}

func (job *Job) ImportEnv(src interface{}) (err error) {
	return job.Env.Import(src)
}

func (job *Job) Environ() map[string]string {
	return job.Env.Map()
}

func (r *JobRunner) SetenvSubEnv(key string, value *Env) error {
	return r.job.SetenvSubEnv(key, value)
}

func (r *JobRunner) SetenvInt64(key string, value int64) {
	r.job.SetenvInt64(key, value)
}

func (r *JobRunner) SetenvInt(key string, value int) {
	r.job.SetenvInt(key, value)
}

func (r *JobRunner) SetenvJson(key string, value interface{}) error {
	return r.job.SetenvJson(key, value)
}

func (r *JobRunner) SetenvList(key string, value []string) error {
	return r.job.SetenvList(key, value)
}

func (r *JobRunner) Setenv(key, value string) {
	r.job.Setenv(key, value)
}
