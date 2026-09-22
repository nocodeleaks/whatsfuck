package whatsmeow

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	waBinary "github.com/nocodeleaks/whatsfuck/binary"
	"github.com/nocodeleaks/whatsfuck/store"
	"github.com/nocodeleaks/whatsfuck/types"
	"github.com/nocodeleaks/whatsfuck/types/events"
	waLog "github.com/nocodeleaks/whatsfuck/util/log"
)

// The connection closes after select dequeues a node but before Err is checked.
// This makes the cancellation race deterministic without relying on scheduling.
type cancelAfterDequeueContext struct {
	context.Context
	cancel context.CancelFunc
}

func (ctx cancelAfterDequeueContext) Err() error {
	ctx.cancel()
	return ctx.Context.Err()
}

func TestHandlerQueueHandlesDequeuedStreamErrorAfterCancellation(t *testing.T) {
	cli := NewClient(&store.Device{}, waLog.Noop)
	cli.DisableLoginAutoReconnect = true
	cli.isLoggedIn.Store(true)
	reconnects := make(chan struct{}, 1)
	cli.AddEventHandler(func(evt any) {
		if _, ok := evt.(*events.ManualLoginReconnect); ok {
			reconnects <- struct{}{}
		}
	})
	queue := make(chan *waBinary.Node, 1)
	queue <- &waBinary.Node{Tag: "stream:error", Attrs: waBinary.Attrs{"code": "515"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	cli.handlerQueueLoop(context.Background(), cancelAfterDequeueContext{ctx, cancel}, queue, done)
	select {
	case <-reconnects:
	case <-time.After(time.Second):
		t.Fatal("lost dequeued stream error")
	}
	if cli.IsLoggedIn() {
		t.Fatal("stream error did not clear the logged-in state")
	}
	select {
	case <-done:
	default:
		t.Fatal("handler queue did not signal completion")
	}
}

func TestHandlerQueueDrainsOnlyStreamErrorsAfterCancellation(t *testing.T) {
	cli := NewClient(&store.Device{}, waLog.Noop)
	cli.DisableLoginAutoReconnect = true
	reconnects := make(chan struct{}, 2)
	presences := make(chan struct{}, 1)
	cli.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.ManualLoginReconnect:
			reconnects <- struct{}{}
		case *events.Presence:
			presences <- struct{}{}
		}
	})
	queue := make(chan *waBinary.Node, 3)
	queue <- &waBinary.Node{Tag: "presence", Attrs: waBinary.Attrs{"from": types.NewJID("1234", types.DefaultUserServer)}}
	queue <- &waBinary.Node{Tag: "stream:error", Attrs: waBinary.Attrs{"code": "515"}}
	queue <- &waBinary.Node{Tag: "stream:error", Attrs: waBinary.Attrs{"code": "515"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cli.handlerQueueLoop(context.Background(), ctx, queue, make(chan struct{}))
	for range 2 {
		select {
		case <-reconnects:
		case <-time.After(time.Second):
			t.Fatal("lost a queued stream error")
		}
	}
	if len(presences) != 0 || len(queue) != 0 {
		t.Fatalf("unexpected canceled queue result: presences=%d remaining=%d", len(presences), len(queue))
	}
}

func TestHandlerQueueWaitsForInFlightStreamErrorPolicy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cli := NewClient(&store.Device{}, waLog.Noop)
		refreshStarted, releaseRefresh := make(chan struct{}), make(chan struct{})
		cli.RefreshCAT = func(context.Context) error {
			close(refreshStarted)
			<-releaseRefresh
			return errors.New("synthetic CAT refresh failure")
		}
		queue := make(chan *waBinary.Node, 1)
		queue <- &waBinary.Node{Tag: "stream:error", Attrs: waBinary.Attrs{"code": events.ConnectFailureCATInvalid.NumberString()}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go cli.handlerQueueLoop(context.Background(), ctx, queue, done)
		<-refreshStarted
		cancel()
		synctest.Wait()
		select {
		case <-done:
			t.Error("queue finished before the stream error decided whether to reconnect")
		default:
		}
		close(releaseRefresh)
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("queue did not finish after the stream error policy was ready")
		}
		if !cli.isExpectedDisconnect() {
			t.Fatal("failed CAT refresh did not prevent reconnect before queue completion")
		}
	})
}

func TestHandlerQueueAllowsReentrantManualReconnectCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cli := NewClient(&store.Device{}, waLog.Noop)
		cli.DisableLoginAutoReconnect = true
		done := make(chan struct{})
		cli.handlerQueueWait = done
		callbackFinished := make(chan struct{})
		startedAt := time.Now()
		cli.AddEventHandler(func(evt any) {
			if _, ok := evt.(*events.ManualLoginReconnect); ok {
				cli.Disconnect()
				close(callbackFinished)
			}
		})
		queue := make(chan *waBinary.Node, 1)
		queue <- &waBinary.Node{Tag: "stream:error", Attrs: waBinary.Attrs{"code": "515"}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		go cli.handlerQueueLoop(context.Background(), ctx, queue, done)
		<-callbackFinished
		if time.Since(startedAt) != 0 {
			t.Fatal("reentrant disconnect had to time out waiting for its own callback")
		}
	})
}

func TestHandlerQueueInternalHookKeepsLegacySignature(t *testing.T) {
	cli := NewClient(&store.Device{}, waLog.Noop)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cli.DangerousInternals().HandlerQueueLoop(context.Background(), ctx, make(chan *waBinary.Node))
}

func TestDisconnectDoesNotHoldSocketLockWhileDrainingHandlers(t *testing.T) {
	cli := NewClient(&store.Device{}, waLog.Noop)
	queueDone := make(chan struct{})
	cli.handlerQueueWait = queueDone
	disconnected := make(chan struct{})
	go func() {
		cli.Disconnect()
		close(disconnected)
	}()
	<-cli.expectedDisconnect.GetChan()
	lockAvailable := make(chan struct{})
	go func() {
		cli.socketLock.RLock()
		hasSocket := cli.socket != nil
		cli.socketLock.RUnlock()
		if hasSocket {
			t.Error("disconnect left the old socket attached")
		}
		close(lockAvailable)
	}()
	select {
	case <-lockAvailable:
	case <-time.After(time.Second):
		close(queueDone)
		<-disconnected
		t.Fatal("disconnect held socketLock while a stream error handler needed it")
	}
	close(queueDone)
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not finish after the handler queue drained")
	}
}

type connectionReviewTransport func(*http.Request) (*http.Response, error)

func (fn connectionReviewTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestConnectCancellationStopsHandlerQueueWaitBeforeDial(t *testing.T) {
	cli := NewClient(&store.Device{}, waLog.Noop)
	cli.InitialAutoReconnect = false
	cli.handlerQueueWait = make(chan struct{})
	var requests int
	cli.preLoginHTTP = &http.Client{Transport: connectionReviewTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected websocket request")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cli.ConnectContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("connect did not honor cancellation while draining the old queue: %v", err)
	}
	if requests != 0 {
		t.Fatal("connect dialed before the previous handler queue finished")
	}
}

type observedQueueWaitContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (ctx *observedQueueWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}

func TestAutoReconnectHonorsExpectedDisconnectFromDrainedQueue(t *testing.T) {
	id := types.NewJID("1234", types.DefaultUserServer)
	cli := NewClient(&store.Device{ID: &id}, waLog.Noop)
	cli.EnableAutoReconnect = true
	cli.handlerQueueWait = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitCtx := &observedQueueWaitContext{Context: ctx, observed: make(chan struct{})}
	finished := make(chan struct{})
	go func() {
		cli.autoReconnect(waitCtx)
		close(finished)
	}()
	select {
	case <-waitCtx.observed:
	case <-time.After(time.Second):
		t.Fatal("autoreconnect did not wait for the previous handler queue")
	}
	// A queued stream:error can change the reconnect policy after the socket
	// has closed. The next connection must respect the completed handler result.
	cli.expectDisconnect()
	close(cli.handlerQueueWait)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("autoreconnect did not stop after an expected disconnect")
	}
	if cli.AutoReconnectErrors != 0 {
		t.Fatal("autoreconnect started a new attempt before checking the drained queue")
	}
}

func TestCompanionMetaNonceInternalHookUsesForkPersistence(t *testing.T) {
	container := &historySyncDeviceContainer{putContextErr: make(chan error, 2)}
	cli := NewClient(&store.Device{Container: container}, waLog.Noop)
	cli.DangerousInternals().StoreCompanionMetaNonce(context.Background(), "fresh")
	if cli.currentCompanionMetaNonce() != "fresh" || cli.Store.CompanionMetaNonce != "fresh" {
		t.Fatal("internal nonce hook did not update the fork's live and persisted nonce")
	}
	cli.DangerousInternals().StoreCompanionMetaNonce(context.Background(), "fresh")
	cli.DangerousInternals().StoreCompanionMetaNonce(context.Background(), "")
	if len(container.putContextErr) != 1 {
		t.Fatal("internal nonce hook persisted duplicate or empty nonce updates")
	}
}

func TestQRChannelContinuesAfterADVSecretRotation(t *testing.T) {
	oldSecret := bytes.Repeat([]byte{1}, 32)
	cli := NewClient(&store.Device{AdvSecretKey: oldSecret}, waLog.Noop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output, err := cli.GetQRChannel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldEncoded := base64.StdEncoding.EncodeToString(oldSecret)
	codePrefix := "test-ref,test-noise,test-identity,"
	cli.dispatchEvent(&events.QR{Codes: []string{codePrefix + oldEncoded, "next-ref,test-noise,test-identity," + oldEncoded}})
	readCode := func(want string) {
		t.Helper()
		select {
		case item, ok := <-output:
			if !ok || item.Event != QRChannelEventCode || item.Code != want {
				t.Fatalf("expected updated QR code, got event=%q code=%q open=%t", item.Event, item.Code, ok)
			}
		case <-time.After(time.Second):
			t.Fatal("QR channel did not emit the rotated code")
		}
	}
	readCode(codePrefix + oldEncoded)
	for range 2 {
		previousSecret := bytes.Clone(cli.Store.AdvSecretKey)
		cli.rotateADVSecret(ctx)
		if len(cli.Store.AdvSecretKey) != 32 || bytes.Equal(previousSecret, cli.Store.AdvSecretKey) {
			t.Fatal("ADV secret was not replaced with a fresh 32-byte key")
		}
		readCode(codePrefix + base64.StdEncoding.EncodeToString(cli.Store.AdvSecretKey))
	}
	cli.dispatchEvent(&events.PairSuccess{})
	if item, ok := <-output; !ok || item != QRChannelSuccess {
		t.Fatalf("pairing did not complete after rotation: event=%q open=%t", item.Event, ok)
	}
	if _, ok := <-output; ok {
		t.Fatal("QR channel remained open after pairing succeeded")
	}
}
