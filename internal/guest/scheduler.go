package guest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"example.com/ish-go/internal/i386"
)

type Task struct {
	Process *Process
	Reaped  bool
}

type Scheduler struct {
	mu      sync.Mutex
	tasks   map[int32]*Task
	nextPID int32
	order   []int32
	steps   uint64
}

func NewScheduler(root *Process) *Scheduler {
	if root.PID == 0 {
		root.PID = 1
	}
	s := &Scheduler{tasks: make(map[int32]*Task), nextPID: root.PID + 1}
	s.tasks[root.PID] = &Task{Process: root}
	s.order = append(s.order, root.PID)
	s.wire(root)
	return s
}

func (s *Scheduler) wire(p *Process) {
	p.Kernel.SetSchedulerAware(true)
	p.Kernel.OnFork = func(cpu *i386.CPU) (int32, error) {
		return s.fork(p.PID)
	}
	p.Kernel.OnWait4 = func(cpu *i386.CPU, pid int32, statusAddr, options uint32) int32 {
		return s.wait4(p, cpu, pid, statusAddr, options)
	}
}

func (s *Scheduler) fork(parentPID int32) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parentTask, ok := s.tasks[parentPID]
	if !ok || parentTask.Process.State == Exited {
		return 0, fmt.Errorf("fork: parent %d is not runnable", parentPID)
	}
	pid := s.nextPID
	s.nextPID++
	child := parentTask.Process.CloneForFork(pid)
	s.tasks[pid] = &Task{Process: child}
	s.order = append(s.order, pid)
	s.wire(child)
	return pid, nil
}

func (s *Scheduler) wait4(parent *Process, cpu *i386.CPU, requested int32, statusAddr, options uint32) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, pid := range s.order {
		task := s.tasks[pid]
		if task == nil || task.Reaped || task.Process.ParentPID != parent.PID {
			continue
		}
		if requested > 0 && requested != task.Process.PID {
			continue
		}
		found = true
		if task.Process.State != Exited {
			if options&1 != 0 { // WNOHANG
				return 0
			}
			return -11 // EAGAIN: scheduler will run the child on the next turn.
		}
		if statusAddr != 0 {
			status := uint32(task.Process.ExitCode&0xff) << 8
			if err := cpu.Mem.Write32(statusAddr, status); err != nil {
				return -14 // EFAULT
			}
		}
		task.Reaped = true
		return task.Process.PID
	}
	if !found {
		return -10 // ECHILD
	}
	return -11
}

func (s *Scheduler) Step() (bool, error) {
	s.mu.Lock()
	order := append([]int32(nil), s.order...)
	s.mu.Unlock()
	for _, pid := range order {
		s.mu.Lock()
		task := s.tasks[pid]
		s.mu.Unlock()
		if task == nil || task.Reaped || task.Process.State == Exited {
			continue
		}
		if task.Process.State == Blocked {
			if !task.Process.Kernel.TryResumeBlockedFutex(task.Process.Image.CPU) {
				continue
			}
			task.Process.State = Ready
		}
		if err := task.Process.Step(); err != nil {
			return false, fmt.Errorf("pid %d: %w", pid, err)
		}
		s.mu.Lock()
		s.steps++
		s.mu.Unlock()
		return true, nil
	}
	return false, nil
}

func (s *Scheduler) Run(ctx context.Context, maxSteps uint64) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if maxSteps != 0 && s.Steps() >= maxSteps {
			return fmt.Errorf("scheduler instruction limit exceeded: %d", maxSteps)
		}
		progress, err := s.Step()
		if err != nil {
			return err
		}
		if !progress {
			if s.hasBlockedTasks() {
				timer := time.NewTimer(time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return nil
		}
	}
}

func (s *Scheduler) hasBlockedTasks() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range s.tasks {
		if task != nil && !task.Reaped && task.Process.State == Blocked {
			return true
		}
	}
	return false
}

func (s *Scheduler) Steps() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.steps
}

func (s *Scheduler) Task(pid int32) (*Process, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[pid]
	if !ok || task.Reaped {
		return nil, false
	}
	return task.Process, true
}

func (s *Scheduler) PIDs() []int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int32, 0, len(s.tasks))
	for pid, task := range s.tasks {
		if task != nil && !task.Reaped {
			out = append(out, pid)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
