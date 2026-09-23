//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const wmAppDiagHeartbeat = 0x80F0

var (
	diagLogCh   chan string
	diagDone    chan struct{}
	diagWG      sync.WaitGroup
	diagStarted atomic.Bool

	diagLastUIActivity atomic.Int64
	diagHeartbeatSent  atomic.Uint64
	diagHeartbeatAck   atomic.Uint64

	diagPaintTotal      atomic.Int64
	diagSizeTotal       atomic.Int64
	diagCommandTotal    atomic.Int64
	diagLayoutTotal     atomic.Int64
	diagInvalidateTotal atomic.Int64

	diagPaintSecond      atomic.Int64
	diagSizeSecond       atomic.Int64
	diagLayoutSecond     atomic.Int64
	diagInvalidateSecond atomic.Int64

	diagCurrentUIOperation     atomic.Value
	diagCurrentWorkerOperation atomic.Value
	diagCurrentDispatch        atomic.Value
	diagDispatchSeq            atomic.Uint64
)

func diagInit() {
	if !diagStarted.CompareAndSwap(false, true) {
		return
	}
	diagLogCh = make(chan string, 16384)
	diagDone = make(chan struct{})
	diagCurrentUIOperation.Store("startup")
	diagCurrentWorkerOperation.Store("idle")
	diagCurrentDispatch.Store("idle")
	diagLastUIActivity.Store(time.Now().UnixNano())

	exe, err := os.Executable()
	logPath := "PLM_Studio_debug.log"
	if err == nil && exe != "" {
		logPath = filepath.Join(filepath.Dir(exe), "PLM_Studio_debug.log")
	}

	diagWG.Add(1)
	go func() {
		defer diagWG.Done()
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return
		}
		defer f.Close()
		bw := bufio.NewWriterSize(f, 64*1024)
		flushTicker := time.NewTicker(250 * time.Millisecond)
		defer flushTicker.Stop()
		for {
			select {
			case line, ok := <-diagLogCh:
				if !ok {
					_ = bw.Flush()
					_ = f.Sync()
					return
				}
				_, _ = bw.WriteString(line)
				_, _ = bw.WriteString("\n")
			case <-flushTicker.C:
				_ = bw.Flush()
			case <-diagDone:
				_ = bw.Flush()
				_ = f.Sync()
			}
		}
	}()

	diagLogf("[BOOT] startup")
	startDiagWatchdog()
}

func diagShutdown() {
	if !diagStarted.Load() {
		return
	}
	diagLogf("[BOOT] diagnostics shutdown")
	close(diagDone)
	time.Sleep(20 * time.Millisecond)
	close(diagLogCh)
	diagWG.Wait()
}

func diagLogf(format string, args ...any) {
	if !diagStarted.Load() || diagLogCh == nil {
		return
	}
	line := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, args...)
	select {
	case diagLogCh <- line:
	default:
		// Diagnostics must never block the UI thread.
	}
}

func diagUIHeartbeat(label string) {
	diagLastUIActivity.Store(time.Now().UnixNano())
	if label != "" {
		diagCurrentUIOperation.Store(label)
	}
}

func diagAckHeartbeat(seq uint64) {
	diagHeartbeatAck.Store(seq)
	diagUIHeartbeat("message-loop")
	diagLogf("[WATCHDOG] UI heartbeat ACK #%d", seq)
}

func startDiagWatchdog() {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				paint := diagPaintSecond.Swap(0)
				size := diagSizeSecond.Swap(0)
				layout := diagLayoutSecond.Swap(0)
				invalidates := diagInvalidateSecond.Swap(0)
				diagLogf("[COUNTERS] 1s paint=%d size=%d layout=%d invalidate=%d", paint, size, layout, invalidates)
				if paint > 200 {
					diagLogf("[WARNING] repaint loop suspected: %d WM_PAINT/sec", paint)
				}
				if size > 200 {
					diagLogf("[WARNING] resize loop suspected: %d WM_SIZE/sec", size)
				}
				if layout > 200 {
					diagLogf("[WARNING] layout loop suspected: %d layout/sec", layout)
				}
				if invalidates > 200 {
					diagLogf("[WARNING] invalidate loop suspected: %d invalidate/sec", invalidates)
				}

				seq := diagHeartbeatSent.Add(1)
				if hwndMain != 0 {
					pPostMessageW.Call(uintptr(hwndMain), wmAppDiagHeartbeat, uintptr(seq), 0)
					diagLogf("[WATCHDOG] UI heartbeat request #%d", seq)
				}

				ack := diagHeartbeatAck.Load()
				last := time.Unix(0, diagLastUIActivity.Load())
				stalled := time.Since(last)
				if hwndMain != 0 && seq > ack+2 && stalled > 2*time.Second {
					uiOp, _ := diagCurrentUIOperation.Load().(string)
					workerOp, _ := diagCurrentWorkerOperation.Load().(string)
					dispatchOp, _ := diagCurrentDispatch.Load().(string)
					diagLogf("[FREEZE DETECTED] last UI activity=%s ago=%s currentUI=%q currentWorker=%q currentDispatch=%q heartbeat_sent=%d heartbeat_ack=%d",
						last.Format("15:04:05.000"), stalled.Truncate(time.Millisecond), uiOp, workerOp, dispatchOp, seq, ack)
				}
			case <-diagDone:
				return
			}
		}
	}()
}

func diagEnterUI(name string) func() {
	prev, _ := diagCurrentUIOperation.Load().(string)
	diagCurrentUIOperation.Store(name)
	diagUIHeartbeat(name)
	start := time.Now()
	return func() {
		d := time.Since(start)
		if d > time.Second {
			diagLogf("[BLOCKING] %s duration=%s", name, d.Truncate(time.Millisecond))
		} else if d > 100*time.Millisecond {
			diagLogf("[SLOW] %s duration=%s", name, d.Truncate(time.Millisecond))
		}
		diagCurrentUIOperation.Store(prev)
		diagLastUIActivity.Store(time.Now().UnixNano())
	}
}

func diagEnterWorker(name string) func() {
	prev, _ := diagCurrentWorkerOperation.Load().(string)
	diagCurrentWorkerOperation.Store(name)
	start := time.Now()
	diagLogf("[WORKER] %s START", name)
	return func() {
		d := time.Since(start)
		if d > time.Second {
			diagLogf("[BLOCKING] [WORKER] %s duration=%s", name, d.Truncate(time.Millisecond))
		} else if d > 100*time.Millisecond {
			diagLogf("[SLOW] [WORKER] %s duration=%s", name, d.Truncate(time.Millisecond))
		}
		diagLogf("[WORKER] %s END duration=%s", name, d.Truncate(time.Millisecond))
		diagCurrentWorkerOperation.Store(prev)
	}
}

func diagWMSizeStart() (int64, time.Time) {
	n := diagSizeTotal.Add(1)
	diagSizeSecond.Add(1)
	diagUIHeartbeat("WM_SIZE")
	diagLogf("[UI] WM_SIZE #%d START", n)
	return n, time.Now()
}

func diagWMSizeEnd(n int64, start time.Time) {
	d := time.Since(start)
	diagLogf("[UI] WM_SIZE #%d END duration=%s", n, d.Truncate(time.Millisecond))
	if d > time.Second {
		diagLogf("[BLOCKING] WM_SIZE #%d duration=%s", n, d.Truncate(time.Millisecond))
	} else if d > 100*time.Millisecond {
		diagLogf("[SLOW] WM_SIZE #%d duration=%s", n, d.Truncate(time.Millisecond))
	}
}

func diagWMPaintStart(window string) (int64, time.Time) {
	n := diagPaintTotal.Add(1)
	diagPaintSecond.Add(1)
	diagUIHeartbeat("WM_PAINT:" + window)
	diagLogf("[UI] WM_PAINT #%d %s START", n, window)
	return n, time.Now()
}

func diagWMPaintEnd(n int64, window string, start time.Time) {
	d := time.Since(start)
	diagLogf("[UI] WM_PAINT #%d %s END duration=%s", n, window, d.Truncate(time.Millisecond))
	if d > time.Second {
		diagLogf("[BLOCKING] WM_PAINT #%d %s duration=%s", n, window, d.Truncate(time.Millisecond))
	} else if d > 100*time.Millisecond {
		diagLogf("[SLOW] WM_PAINT #%d %s duration=%s", n, window, d.Truncate(time.Millisecond))
	}
}

func diagWMCommand(id, notify uint16) {
	n := diagCommandTotal.Add(1)
	diagUIHeartbeat("WM_COMMAND")
	diagLogf("[UI] WM_COMMAND #%d id=%d notify=%d", n, id, notify)
}

func diagLayoutStart() (int64, time.Time) {
	n := diagLayoutTotal.Add(1)
	diagLayoutSecond.Add(1)
	diagCurrentUIOperation.Store("layout")
	diagLastUIActivity.Store(time.Now().UnixNano())
	diagLogf("[UI] layout() #%d START", n)
	return n, time.Now()
}

func diagLayoutEnd(n int64, start time.Time) {
	d := time.Since(start)
	diagLogf("[UI] layout() #%d END duration=%s", n, d.Truncate(time.Millisecond))
	if d > time.Second {
		diagLogf("[BLOCKING] layout() #%d duration=%s", n, d.Truncate(time.Millisecond))
	} else if d > 100*time.Millisecond {
		diagLogf("[SLOW] layout() #%d duration=%s", n, d.Truncate(time.Millisecond))
	}
	diagCurrentUIOperation.Store("message-loop")
	diagLastUIActivity.Store(time.Now().UnixNano())
}

func diagInvalidate() {
	diagInvalidateTotal.Add(1)
	diagInvalidateSecond.Add(1)
}

func diagFunctionStart(scope, name string) time.Time {
	if scope == "UI" {
		diagCurrentUIOperation.Store(name)
		diagLastUIActivity.Store(time.Now().UnixNano())
	} else {
		diagCurrentWorkerOperation.Store(name)
	}
	diagLogf("[%s] %s START", scope, name)
	return time.Now()
}

func diagFunctionEnd(scope, name string, start time.Time) {
	d := time.Since(start)
	diagLogf("[%s] %s END duration=%s", scope, name, d.Truncate(time.Millisecond))
	if d > time.Second {
		diagLogf("[BLOCKING] [%s] %s duration=%s", scope, name, d.Truncate(time.Millisecond))
	} else if d > 100*time.Millisecond {
		diagLogf("[SLOW] [%s] %s duration=%s", scope, name, d.Truncate(time.Millisecond))
	}
	if scope == "UI" {
		diagCurrentUIOperation.Store("message-loop")
		diagLastUIActivity.Store(time.Now().UnixNano())
	} else {
		diagCurrentWorkerOperation.Store("idle")
	}
}

func diagWindowIdentity(hwnd syscall.Handle) string {
	if hwnd == 0 {
		return "hwnd=0"
	}
	buf := make([]uint16, 128)
	n, _, _ := pGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	className := "?"
	if n > 0 {
		className = syscall.UTF16ToString(buf[:n])
	}
	id, _, _ := pGetDlgCtrlID.Call(uintptr(hwnd))
	return fmt.Sprintf("hwnd=0x%X class=%s id=%d", uintptr(hwnd), className, int32(id))
}

func diagMessageName(msg uint32) string {
	switch msg {
	case WM_SIZE:
		return "WM_SIZE"
	case WM_PAINT:
		return "WM_PAINT"
	case WM_COMMAND:
		return "WM_COMMAND"
	case WM_HSCROLL:
		return "WM_HSCROLL"
	case WM_VSCROLL:
		return "WM_VSCROLL"
	case WM_LBUTTONDOWN:
		return "WM_LBUTTONDOWN"
	case WM_LBUTTONUP:
		return "WM_LBUTTONUP"
	case WM_RBUTTONDOWN:
		return "WM_RBUTTONDOWN"
	case WM_MOUSEMOVE:
		return "WM_MOUSEMOVE"
	case WM_DESTROY:
		return "WM_DESTROY"
	case wmAppDiagHeartbeat:
		return "WM_APP_DIAG_HEARTBEAT"
	case wmAppMapLoadProgress:
		return "WM_APP_MAP_LOAD_PROGRESS"
	case wmAppMapLoadDone:
		return "WM_APP_MAP_LOAD_DONE"
	default:
		return fmt.Sprintf("0x%04X", msg)
	}
}

func diagBeforeDispatch(m *MSG) {
	if m == nil {
		return
	}
	seq := diagDispatchSeq.Add(1)
	entry := fmt.Sprintf("#%d %s msg=%s(%#x) w=%#x l=%#x", seq, diagWindowIdentity(m.Hwnd), diagMessageName(m.Message), m.Message, m.WParam, m.LParam)
	diagCurrentDispatch.Store(entry)
	// Log only non-noisy messages. The watchdog always records currentDispatch,
	// so even a noisy message that hangs is still identified precisely.
	if m.Message != WM_MOUSEMOVE && m.Message != WM_PAINT {
		diagLogf("[DISPATCH] START %s", entry)
	}
}

func diagAfterDispatch(m *MSG) {
	if m == nil {
		return
	}
	entry, _ := diagCurrentDispatch.Load().(string)
	if m.Message != WM_MOUSEMOVE && m.Message != WM_PAINT {
		diagLogf("[DISPATCH] END %s", entry)
	}
	diagCurrentDispatch.Store("idle")
	diagLastUIActivity.Store(time.Now().UnixNano())
}
