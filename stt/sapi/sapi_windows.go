//go:build windows

package sapi

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/grammar"
	"github.com/mrlm-net/voice-goio/normalise"
)

// callsignRule is the name of the dynamic rule in atc.grxml that is rebuilt
// from the aircraft on frequency, and transmissionRule is the root rule that is
// activated while the push to talk key is held.
const (
	callsignRule     = "callsign"
	transmissionRule = "transmission"
	callsignProperty = "callsign"
)

// Recognizer is the SAPI 5 implementation of voicegoio.STT.
//
// All COM work happens on one goroutine locked to one OS thread. COM apartment
// rules require it, and it also means the interface pointers need no locking:
// public methods post a closure to that goroutine and wait for its result.
type Recognizer struct {
	opt Options

	cmds    chan func()
	results chan voicegoio.Recognition
	done    chan struct{}
	ready   chan error
	wg      sync.WaitGroup

	closeOnce sync.Once

	// owned by the COM goroutine
	rec     comObject
	ctx     comObject
	gram    comObject
	tmpGram string
	got     bool // a result was delivered during the current PTT cycle
}

// New creates the recognizer, opens the microphone and loads the grammar.
//
// It fails fast with ErrNoSpeechPack when Windows has no English recognition
// engine, because that is a setup problem the user has to fix in Settings and
// no amount of retrying will help.
func New(opt Options) (*Recognizer, error) {
	opt.withDefaults()
	r := &Recognizer{
		opt:     opt,
		cmds:    make(chan func()),
		results: make(chan voicegoio.Recognition, 4),
		done:    make(chan struct{}),
		ready:   make(chan error, 1),
	}
	r.wg.Add(1)
	go r.loop()
	if err := <-r.ready; err != nil {
		r.wg.Wait()
		return nil, err
	}
	return r, nil
}

// do runs fn on the COM goroutine and waits for it.
func (r *Recognizer) do(fn func() error) error {
	errc := make(chan error, 1)
	select {
	case r.cmds <- func() { errc <- fn() }:
		return <-errc
	case <-r.done:
		return voicegoio.ErrClosed
	}
}

// loop owns every COM object for the lifetime of the recogniser.
func (r *Recognizer) loop() {
	defer r.wg.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := coInitialize(); err != nil {
		r.ready <- err
		return
	}
	defer coUninitialize()

	if err := r.setup(); err != nil {
		r.teardown()
		r.ready <- err
		return
	}
	r.ready <- nil
	defer r.teardown()

	for {
		select {
		case <-r.done:
			return
		case fn := <-r.cmds:
			fn()
		default:
			// Poll for engine events. The 50 ms timeout bounds how long a
			// posted command waits behind the event wait.
			r.pump(50)
		}
	}
}

// setup builds the recognizer, the context, the audio input and the grammar.
func (r *Recognizer) setup() error {
	rec, err := coCreateInstance(&clsidSpInprocRecognizer, &iidISpRecognizer)
	if err != nil {
		return fmt.Errorf("sapi: create in-process recognizer: %w", err)
	}
	r.rec = rec

	if err := r.selectRecognizerToken(); err != nil {
		return err
	}
	if err := r.selectAudioInput(); err != nil {
		return err
	}

	var ctx comObject
	if err := r.rec.call("ISpRecognizer::CreateRecoContext", recCreateRecoContext, up(&ctx)); err != nil {
		return err
	}
	r.ctx = ctx

	// Notification by Win32 event: no COM event sink, so no foreign thread ever
	// calls back into Go.
	if err := r.ctx.call("ISpRecoContext::SetNotifyWin32Event", ctxSetNotifyWin32Event); err != nil {
		return err
	}
	interest := uint64(1)<<speiRecognition | uint64(1)<<speiFalseRecognition | uint64(1)<<speiHypothesis
	if err := r.ctx.call("ISpRecoContext::SetInterest", ctxSetInterest, uintptr(interest), uintptr(interest)); err != nil {
		return err
	}

	if err := r.loadGrammar(); err != nil {
		return err
	}
	// Idle until the first PTT press.
	return r.rec.call("ISpRecognizer::SetRecoState", recSetRecoState, sprstInactive)
}

// selectRecognizerToken picks the speech engine. Without an English engine
// installed there is nothing to recognise with, so this is where the typed
// setup error comes from.
func (r *Recognizer) selectRecognizerToken() error {
	attrs := "Language=409|809" // en-US or en-GB
	switch strings.ToLower(r.opt.Locale) {
	case "en-us":
		attrs = "Language=409"
	case "en-gb":
		attrs = "Language=809"
	case "":
	default:
		attrs = ""
	}
	tokens, err := enumTokens(catRecognizers, attrs)
	if err != nil {
		return err
	}
	defer releaseAll(tokens)
	if len(tokens) == 0 {
		return ErrNoSpeechPack
	}
	return r.rec.call("ISpRecognizer::SetRecognizer", recSetRecognizer, obj(tokens[0]))
}

// selectAudioInput binds the microphone. SPEC.md 4.4 requires the input to be
// selectable, because a user with a headset and a desk mic has to be able to
// say which one is the PTT microphone.
func (r *Recognizer) selectAudioInput() error {
	tokens, err := enumTokens(catAudioIn, "")
	if err != nil {
		return err
	}
	defer releaseAll(tokens)
	if len(tokens) == 0 {
		return ErrNoAudioInput
	}
	chosen := tokens[0]
	if r.opt.AudioInputID != "" {
		found := false
		for _, t := range tokens {
			if id, _ := tokenID(t); id == r.opt.AudioInputID {
				chosen, found = t, true
				break
			}
		}
		if !found {
			return fmt.Errorf("sapi: audio input %q not found", r.opt.AudioInputID)
		}
	}
	return r.rec.call("ISpRecognizer::SetInput", recSetInput, obj(chosen), 1)
}

// loadGrammar compiles atc.grxml. SAPI compiles SRGS XML only from a file, so
// the embedded grammar is written to a temporary file first.
func (r *Recognizer) loadGrammar() error {
	var g comObject
	if err := r.ctx.call("ISpRecoContext::CreateGrammar", ctxCreateGrammar, 1, 0, up(&g)); err != nil {
		return err
	}
	r.gram = g

	path := r.opt.GrammarPath
	if path == "" {
		f, err := os.CreateTemp("", "voicegoio-*.grxml")
		if err != nil {
			return fmt.Errorf("sapi: write grammar: %w", err)
		}
		if _, err := f.WriteString(grammar.ATC); err != nil {
			f.Close()
			return fmt.Errorf("sapi: write grammar: %w", err)
		}
		f.Close()
		path = f.Name()
		r.tmpGram = path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	p, keep := utf16(abs)
	err = r.gram.call("ISpRecoGrammar::LoadCmdFromFile", grLoadCmdFromFile, up(p), sploDynamic)
	runtime.KeepAlive(keep)
	if err != nil {
		return fmt.Errorf("sapi: load %s: %w", abs, err)
	}
	return nil
}

// SetCallsigns rebuilds the dynamic callsign rule.
//
// This is the accuracy lever: instead of recognising any callsign in the world,
// the engine only has to tell apart the handful of aircraft actually on the
// frequency. Every phraseology variant is added, so a pilot may say "tree" or
// "three".
func (r *Recognizer) SetCallsigns(cs []voicegoio.Callsign) error {
	return r.do(func() error {
		name, keepName := utf16(callsignRule)
		defer runtime.KeepAlive(keepName)

		var state uintptr
		if err := r.gram.call("ISpGrammarBuilder::GetRule", gbGetRule,
			up(name), 0, sprafTopLevel|sprafDynamic, 1, up(&state)); err != nil {
			return err
		}
		if err := r.gram.call("ISpGrammarBuilder::ClearRule", gbClearRule, state); err != nil {
			return err
		}

		propName, keepProp := utf16(callsignProperty)
		defer runtime.KeepAlive(keepProp)
		sep, keepSep := utf16(" ")
		defer runtime.KeepAlive(keepSep)

		for _, c := range cs {
			spoken := c.Spoken
			if spoken == "" {
				spoken = normalise.SpokenCallsign(c.ICAO)
			}
			icao, keepICAO := utf16(strings.ToUpper(c.ICAO))
			prop := spPropertyInfo{pszName: propName, pszValue: icao}
			for _, variant := range normalise.SpokenVariants(spoken) {
				words, keepWords := utf16(variant)
				// Weight is argument seven, which the x64 ABI passes on the
				// stack, so its float bits can travel in a uintptr.
				err := r.gram.call("ISpGrammarBuilder::AddWordTransition", gbAddWordTransition,
					state, 0, up(words), up(sep), 1 /*SPWT_LEXICAL*/, uintptr(math.Float32bits(1.0)),
					up(&prop))
				runtime.KeepAlive(keepWords)
				if err != nil {
					runtime.KeepAlive(keepICAO)
					return err
				}
			}
			runtime.KeepAlive(keepICAO)
		}
		return r.gram.call("ISpGrammarBuilder::Commit", gbCommit, 0)
	})
}

// Start is called when the push to talk key goes down.
func (r *Recognizer) Start() error {
	return r.do(func() error {
		r.got = false
		if err := r.rec.call("ISpRecognizer::SetRecoState", recSetRecoState, sprstActive); err != nil {
			return err
		}
		name, keep := utf16(transmissionRule)
		defer runtime.KeepAlive(keep)
		if err := r.gram.call("ISpRecoGrammar::SetRuleState", grSetRuleState, up(name), 0, sprsActive); err != nil {
			return err
		}
		return r.ctx.call("ISpRecoContext::Resume", ctxResume, 0)
	})
}

// Stop is called when the key is released. It waits briefly for the engine to
// finish the utterance in flight, then reports say_again if nothing arrived:
// the application must always get exactly one result per cycle.
func (r *Recognizer) Stop() error {
	return r.do(func() error {
		name, keep := utf16(transmissionRule)
		defer runtime.KeepAlive(keep)
		if err := r.gram.call("ISpRecoGrammar::SetRuleState", grSetRuleState, up(name), 0, sprsInactive); err != nil {
			return err
		}
		deadline := time.Now().Add(time.Duration(r.opt.FinalResultTimeoutMS) * time.Millisecond)
		for !r.got && time.Now().Before(deadline) {
			r.pump(50)
		}
		if !r.got {
			r.publish(voicegoio.Recognition{
				Tags: map[string]string{voicegoio.TagIntent: voicegoio.IntentSayAgain},
			})
		}
		// Release the microphone between transmissions.
		return r.rec.call("ISpRecognizer::SetRecoState", recSetRecoState, sprstInactive)
	})
}

// pump waits up to timeoutMS for an engine notification and drains the events
// behind it. Called from the COM goroutine only.
func (r *Recognizer) pump(timeoutMS uintptr) {
	if r.ctx == nil {
		return
	}
	if hr := r.ctx.raw(ctxWaitForNotifyEvent, timeoutMS); hr != sOK {
		return // S_FALSE means the wait timed out
	}
	for {
		var ev spEvent
		var fetched uint32
		hr := r.ctx.raw(ctxGetEvents, 1, up(&ev), up(&fetched))
		if int32(hr) < 0 || fetched == 0 {
			return
		}
		switch ev.eEventID {
		case speiRecognition:
			res := comObject(ev.lParam)
			r.publish(r.recognition(res))
			r.got = true
			res.release()
		case speiFalseRecognition:
			if ev.lParam != nil {
				comObject(ev.lParam).release()
			}
			r.publish(voicegoio.Recognition{
				Tags: map[string]string{voicegoio.TagIntent: voicegoio.IntentSayAgain},
			})
			r.got = true
		default:
			// Hypotheses and anything else are released and ignored.
			if ev.elParamType == 2 /*SPET_LPARAM_IS_OBJECT*/ && ev.lParam != nil {
				comObject(ev.lParam).release()
			}
		}
	}
}

// recognition converts an ISpRecoResult into the application level result.
func (r *Recognizer) recognition(res comObject) voicegoio.Recognition {
	out := voicegoio.Recognition{Tags: map[string]string{}}

	var text *uint16
	if err := res.call("ISpPhrase::GetText", phGetText,
		0xFFFFFFFF, 0xFFFFFFFF, 1, up(&text), 0); err == nil {
		out.Text = fromUTF16(text)
		coTaskMemFree(unsafe.Pointer(text))
	}

	var ph *spPhrase
	if err := res.call("ISpPhrase::GetPhrase", phGetPhrase, up(&ph)); err != nil || ph == nil {
		out.Tags[voicegoio.TagIntent] = voicegoio.IntentSayAgain
		return out
	}
	defer coTaskMemFree(unsafe.Pointer(ph))

	out.Confidence = ph.rule.sREngineConfidence
	collectProperties(ph.pProperties, out.Tags)

	if out.Confidence < r.opt.MinConfidence || len(out.Tags) == 0 {
		// A low confidence result is worse than none: acting on a misheard
		// clearance is the failure mode this threshold exists to prevent.
		out.Tags = map[string]string{voicegoio.TagIntent: voicegoio.IntentSayAgain}
	}
	if _, ok := out.Tags[voicegoio.TagIntent]; !ok {
		out.Tags[voicegoio.TagIntent] = voicegoio.IntentSayAgain
	}
	return out
}

// collectProperties walks the SPPHRASEPROPERTY tree the grammar's <tag>
// elements produced, flattening it into the Tags map.
func collectProperties(prop *spPhraseProperty, into map[string]string) {
	for prop != nil {
		name := fromUTF16(prop.pszName)
		value := fromUTF16(prop.pszValue)
		if name != "" && value != "" {
			if _, exists := into[name]; !exists {
				into[name] = value
			}
		}
		collectProperties(prop.pFirstChild, into)
		prop = prop.pNextSibling
	}
}

func (r *Recognizer) publish(rec voicegoio.Recognition) {
	if rec.Tags == nil {
		rec.Tags = map[string]string{}
	}
	select {
	case r.results <- rec:
	case <-r.done:
	default:
		// The application is not keeping up; the newest result matters most, so
		// drop the oldest and keep this one.
		select {
		case <-r.results:
		default:
		}
		select {
		case r.results <- rec:
		default:
		}
	}
}

// Results delivers one recognition per push to talk cycle.
func (r *Recognizer) Results() <-chan voicegoio.Recognition { return r.results }

// Close releases the engine and the microphone.
func (r *Recognizer) Close() error {
	r.closeOnce.Do(func() { close(r.done) })
	r.wg.Wait()
	return nil
}

func (r *Recognizer) teardown() {
	if r.rec != nil {
		r.rec.raw(recSetRecoState, sprstInactiveWithPurge)
	}
	r.gram.release()
	r.ctx.release()
	r.rec.release()
	r.gram, r.ctx, r.rec = nil, nil, nil
	if r.tmpGram != "" {
		os.Remove(r.tmpGram)
	}
}

// ---- object token helpers --------------------------------------------------

// enumTokens lists the tokens of a SAPI registry category, optionally filtered
// by attribute expression ("Language=409|809").
func enumTokens(category, reqAttrs string) ([]comObject, error) {
	cat, err := coCreateInstance(&clsidSpObjectTokenCategory, &iidISpObjectTokenCategory)
	if err != nil {
		return nil, fmt.Errorf("sapi: token category: %w", err)
	}
	defer cat.release()

	idp, keepID := utf16(category)
	err = cat.call("ISpObjectTokenCategory::SetId", tcSetId, up(idp), 0)
	runtime.KeepAlive(keepID)
	if err != nil {
		return nil, err
	}

	reqp, keepReq := utf16(reqAttrs)
	var enum comObject
	err = cat.call("ISpObjectTokenCategory::EnumTokens", tcEnumTokens, up(reqp), 0, up(&enum))
	runtime.KeepAlive(keepReq)
	if err != nil {
		return nil, err
	}
	defer enum.release()

	var out []comObject
	for {
		var tok comObject
		var fetched uint32
		hr := enum.raw(enumNext, 1, up(&tok), up(&fetched))
		if int32(hr) < 0 || fetched == 0 || tok == nil {
			break
		}
		out = append(out, tok)
	}
	return out, nil
}

func releaseAll(objs []comObject) {
	for _, o := range objs {
		o.release()
	}
}

func tokenID(tok comObject) (string, error) {
	var p *uint16
	if err := tok.call("ISpObjectToken::GetId", tokGetId, up(&p)); err != nil {
		return "", err
	}
	defer coTaskMemFree(unsafe.Pointer(p))
	return fromUTF16(p), nil
}

func tokenDescription(tok comObject) string {
	var p *uint16
	if err := tok.call("ISpDataKey::GetStringValue", dkGetStringValue, 0, up(&p)); err != nil {
		return ""
	}
	defer coTaskMemFree(unsafe.Pointer(p))
	return fromUTF16(p)
}

// InputDevices lists the microphones SAPI can use, for the settings UI.
func InputDevices() ([]voicegoio.Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := coInitialize(); err != nil {
		return nil, err
	}
	defer coUninitialize()

	tokens, err := enumTokens(catAudioIn, "")
	if err != nil {
		return nil, err
	}
	defer releaseAll(tokens)

	out := make([]voicegoio.Device, 0, len(tokens))
	for i, t := range tokens {
		id, _ := tokenID(t)
		out = append(out, voicegoio.Device{ID: id, Name: tokenDescription(t), Default: i == 0})
	}
	return out, nil
}

// Engines lists the installed speech recognition engines, so a setup screen can
// tell the user what Windows actually has.
func Engines() ([]voicegoio.Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := coInitialize(); err != nil {
		return nil, err
	}
	defer coUninitialize()

	tokens, err := enumTokens(catRecognizers, "")
	if err != nil {
		return nil, err
	}
	defer releaseAll(tokens)
	if len(tokens) == 0 {
		return nil, ErrNoSpeechPack
	}
	out := make([]voicegoio.Device, 0, len(tokens))
	for i, t := range tokens {
		id, _ := tokenID(t)
		out = append(out, voicegoio.Device{ID: id, Name: tokenDescription(t), Default: i == 0})
	}
	return out, nil
}

var _ voicegoio.STT = (*Recognizer)(nil)
