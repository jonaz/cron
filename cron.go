// Package cron implements a cron spec parser and runner.  See the README for
// more details.
package cron

import (
	"context"
	"sort"
	"sync/atomic"
	"time"
)

// Cron keeps track of any number of entries, invoking the associated func as
// specified by the schedule. It may be started, stopped, and the entries may
// be inspected while running.
type Cron struct {
	entries   []*Entry
	add       chan *Entry
	remove    chan int64
	removeAll chan struct{}
	snapshot  chan []*Entry
	running   bool
	count     int64
}

// Job is an interface for submitted cron jobs.
type Job interface {
	Run()
}

// The Schedule describes a job's duty cycle.
type Schedule interface {
	// Return the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
	Next(time.Time) time.Time
}

// Entry consists of a schedule and the func to execute on that schedule.
type Entry struct {
	// The schedule on which this job should be run.
	Schedule Schedule

	// The next time the job will run. This is the zero time if Cron has not been
	// started or this entry's schedule is unsatisfiable
	Next time.Time

	// The last time this job was run. This is the zero time if the job has never
	// been run.
	Prev time.Time

	// The Job to run.
	Job Job

	// The identifier to reference the job instance.
	ID int64

	// 0: normal, 1: paused
	Status int
}

// byTime is a wrapper for sorting the entry array by time
// (with zero time at the end).
type byTime []*Entry

func (s byTime) Len() int      { return len(s) }
func (s byTime) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byTime) Less(i, j int) bool {
	// Two zero times should return false.
	// Otherwise, zero is "greater" than any other time.
	// (To sort it at the end of the list.)
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

// New returns a new Cron job runner.
func New() *Cron {
	return &Cron{
		add:       make(chan *Entry),
		snapshot:  make(chan []*Entry),
		remove:    make(chan int64),
		removeAll: make(chan struct{}),
	}
}

// A wrapper that turns a func() into a cron.Job
type FuncJob func()

func (f FuncJob) Run() { f() }

// AddFunc adds a func to the Cron to be run on the given schedule.
func (c *Cron) AddFunc(spec string, cmd func()) (int64, error) {
	return c.AddJob(spec, FuncJob(cmd))
}

// RemoveJob removes a func from the Cron referenced by the id.
func (c *Cron) RemoveJob(id int64) {
	if !c.running {
		return
	}
	select {
	case c.remove <- id:
	case <-time.After(1 * time.Second):
	}
}

// RemoveAll  removes all jobs
func (c *Cron) RemoveAll() {
	if !c.running {
		c.entries = nil
		return
	}

	select {
	case c.removeAll <- struct{}{}:
	case <-time.After(1 * time.Second):
	}
}
func (c *Cron) removeJob(id int64) {
	w := 0 // write index
	for _, x := range c.entries {
		if id == x.ID {
			continue
		}
		c.entries[w] = x
		w++
	}
	c.entries = c.entries[:w]
}

func (c *Cron) PauseFunc(id int64) {
	for _, x := range c.entries {
		if id == x.ID {
			x.Status = 1
			break
		}
	}
}

func (c *Cron) ResumeFunc(id int64) {
	for _, x := range c.entries {
		if id == x.ID {
			x.Status = 0
			break
		}
	}
}

// Status inquires the status of a job, 0: running, 1: paused, -1: not started.
func (c *Cron) Status(id int) int {
	for _, x := range c.entries {
		if id == int(x.ID) {
			return x.Status
		}
	}
	return -1
}

// AddFunc adds a Job to the Cron to be run on the given schedule.
func (c *Cron) AddJob(spec string, cmd Job) (int64, error) {
	schedule, err := Parse(spec)
	if err != nil {
		return -1, err
	}
	atomic.AddInt64(&c.count, 1)
	i := atomic.LoadInt64(&c.count)
	c.Schedule(schedule, cmd, i)
	return i, nil
}

// Schedule adds a Job to the Cron to be run on the given schedule.
func (c *Cron) Schedule(schedule Schedule, cmd Job, id int64) {
	entry := &Entry{
		Schedule: schedule,
		Job:      cmd,
		ID:       id,
		Status:   0,
	}
	if !c.running {
		c.entries = append(c.entries, entry)
		return
	}

	c.add <- entry
}

// Entries returns a snapshot of the cron entries.
func (c *Cron) Entries() []*Entry {
	if c.running {
		c.snapshot <- nil
		x := <-c.snapshot
		return x
	}
	return c.entrySnapshot()
}

// Start the cron scheduler in its own go-routine.
func (c *Cron) Start(ctx context.Context) {
	c.running = true
	go c.run(ctx)
}

// Run the scheduler.. this is private just due to the need to synchronize
// access to the 'running' state variable.
func (c *Cron) run(ctx context.Context) {
	// Figure out the next activation times for each entry.
	now := time.Now().Local()
	for _, entry := range c.entries {
		entry.Next = entry.Schedule.Next(now)
	}

	for {
		// Determine the next entry to run.
		sort.Sort(byTime(c.entries))

		var effective time.Time
		if len(c.entries) == 0 || c.entries[0].Next.IsZero() {
			// If there are no entries yet, just sleep - it still handles new entries
			// and stop requests.
			effective = now.AddDate(10, 0, 0)
		} else {
			effective = c.entries[0].Next
		}

		select {
		case now = <-time.After(effective.Sub(now)):
			// Run every entry whose next time was this effective time.
			for _, e := range c.entries {
				if e.Next != effective {
					break
				}
				if e.Status == 0 {
					go e.Job.Run()
				}
				e.Prev = e.Next
				e.Next = e.Schedule.Next(effective)
			}
			continue

		case newEntry := <-c.add:
			c.entries = append(c.entries, newEntry)
			newEntry.Next = newEntry.Schedule.Next(now)

		case id := <-c.remove:
			c.removeJob(id)
		case <-c.removeAll:
			c.entries = nil
		case <-c.snapshot:
			c.snapshot <- c.entrySnapshot()

		case <-ctx.Done():
			return
		}

		// 'now' should be updated after newEntry and snapshot cases.
		now = time.Now().Local()
	}
}

// entrySnapshot returns a copy of the current cron entry list.
func (c *Cron) entrySnapshot() []*Entry {
	entries := []*Entry{}
	for _, e := range c.entries {
		entries = append(entries, &Entry{
			Schedule: e.Schedule,
			Next:     e.Next,
			Prev:     e.Prev,
			Job:      e.Job,
			ID:       e.ID,
			Status:   e.Status,
		})
	}
	return entries
}
