package main

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// UpdateInfo describes a newer version of the app.
type UpdateInfo struct {
	Version string
	Notes   string
}

// pendingUpdate is the update the feed offered, held until the user installs
// it.
var pendingUpdate struct {
	sync.Mutex
	update *mygo.Update
}

// checkForUpdate asks the update feed for a newer version and offers it. It
// runs off the UI thread, and gives up quietly when it is offline.
func (a *app) checkForUpdate() {
	if !mygo.Updater.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	update, err := mygo.Updater.Check(ctx)
	if err != nil || update == nil {
		return
	}
	pendingUpdate.Lock()
	pendingUpdate.update = update
	pendingUpdate.Unlock()
	a.emit(func() {
		a.update = &UpdateInfo{Version: update.Version, Notes: update.Notes}
		a.updateStage = updateAvailable
		a.dirty = true
	})
}

// startUpdate downloads the newer version, installs it and restarts the app.
func (a *app) startUpdate() {
	pendingUpdate.Lock()
	update := pendingUpdate.update
	pendingUpdate.Unlock()
	if update == nil {
		a.updateStage = updateFailed
		a.updateMessage = "找不到待安装的更新，请重新检查。"
		a.dirty = true
		return
	}
	a.updateStage = updateDownloading
	a.updateProgress = 0
	a.updateMessage = ""
	a.dirty = true
	go func() {
		err := update.Install(context.Background(), func(downloaded, total int64) {
			if total <= 0 {
				return
			}
			progress := float64(downloaded) / float64(total)
			a.emit(func() {
				a.updateProgress = progress
				a.dirty = true
			})
		})
		if err != nil {
			a.emit(func() {
				a.updateStage = updateFailed
				a.updateMessage = err.Error()
				a.dirty = true
			})
			return
		}
		a.emit(func() {
			a.updateStage = updateRestarting
			a.dirty = true
		})
		mygo.App.Relaunch()
	}()
}
