//go:build windows

package sapi

// Minimal COM plumbing: enough to create an in-process SAPI recognizer and call
// its methods by vtable slot.
//
// Every interface below is described by the index of each method in its vtable.
// The indices come from sapi.h: the first three slots are always IUnknown
// (QueryInterface, AddRef, Release), followed by the methods of each base
// interface in declaration order, then the interface's own methods. Getting one
// index wrong calls the wrong function with the wrong arguments, so the
// constants are grouped per interface with the inherited base named.

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	ole32 = syscall.NewLazyDLL("ole32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	sOK    = 0
	sFalse = 1
)

// guid is the Windows GUID layout.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func mustGUID(s string) guid {
	var g guid
	var d4 [8]byte
	n, err := fmt.Sscanf(s, "%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		&g.Data1, &g.Data2, &g.Data3,
		&d4[0], &d4[1], &d4[2], &d4[3], &d4[4], &d4[5], &d4[6], &d4[7])
	if err != nil || n != 11 {
		panic("sapi: bad GUID literal " + s)
	}
	g.Data4 = d4
	return g
}

// SAPI class and interface identifiers (sapi.h).
var (
	clsidSpInprocRecognizer    = mustGUID("41B89B6B-9399-11D2-9623-00C04F8EE628")
	iidISpRecognizer           = mustGUID("C2B5F241-DAA0-4507-9E16-5A1EAA2B7A5C")
	clsidSpObjectTokenCategory = mustGUID("A910187F-0C7A-45AC-92CC-59EDAFB77B53")
	iidISpObjectTokenCategory  = mustGUID("2D3D3845-39AF-4850-BBF9-40B49780011D")

	// SpFileStream lets a WAV file stand in for the microphone, which is what
	// makes the recognition corpus runnable without a human saying every
	// phrase into a headset.
	clsidSpFileStream = mustGUID("947812B3-2AE1-4644-BA86-9E90DED7EC91")
	iidISpStream      = mustGUID("12E3CCA9-7518-44C5-A5E7-BA5A79CB929E")
)

// Registry category ids for the token enumerator.
const (
	catAudioIn     = `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Speech\AudioInput`
	catRecognizers = `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Speech\Recognizers`
)

// ---- vtable slot indices ---------------------------------------------------

// IUnknown, the base of everything.
const (
	unkQueryInterface = 0
	unkAddRef         = 1
	unkRelease        = 2
)

// ISpRecognizer : ISpProperties : IUnknown.
// ISpProperties contributes slots 3..6.
const (
	recSetRecognizer     = 7
	recGetRecognizer     = 8
	recSetInput          = 9
	recGetInputObjToken  = 10
	recGetInputStream    = 11
	recCreateRecoContext = 12
	recGetRecoProfile    = 13
	recSetRecoProfile    = 14
	recIsSharedInstance  = 15
	recGetRecoState      = 16
	recSetRecoState      = 17
	recGetStatus         = 18
)

// ISpRecoContext : ISpEventSource : ISpNotifySource : IUnknown.
// ISpNotifySource contributes 3..9, ISpEventSource 10..12.
const (
	ctxSetNotifyWin32Event  = 7
	ctxWaitForNotifyEvent   = 8
	ctxGetNotifyEventHandle = 9
	ctxSetInterest          = 10
	ctxGetEvents            = 11
	ctxGetInfo              = 12
	ctxGetRecognizer        = 13
	ctxCreateGrammar        = 14
	ctxGetStatus            = 15
	ctxGetMaxAlternates     = 16
	ctxSetMaxAlternates     = 17
	ctxSetAudioOptions      = 18
	ctxPause                = 23
	ctxResume               = 24
)

// ISpRecoGrammar : ISpGrammarBuilder : IUnknown.
// ISpGrammarBuilder contributes 3..10.
const (
	gbResetGrammar      = 3
	gbGetRule           = 4
	gbClearRule         = 5
	gbCreateNewState    = 6
	gbAddWordTransition = 7
	gbAddRuleTransition = 8
	gbAddResource       = 9
	gbCommit            = 10

	grGetGrammarId    = 11
	grGetRecoContext  = 12
	grLoadCmdFromFile = 13
	grSetRuleState    = 18
	grSetRuleIdState  = 19
	grSetGrammarState = 26
)

// ISpRecoResult : ISpPhrase : IUnknown.
const (
	phGetPhrase = 3
	phGetText   = 5
	phDiscard   = 6
)

// ISpStream : ISpStreamFormat : ISequentialStream : IStream : IUnknown.
// IStream contributes Read/Write (3..4), Seek/SetSize/CopyTo/Commit/Revert/
// LockRegion/UnlockRegion/Stat/Clone (5..13); ISpStreamFormat adds
// GetFormat (14); ISpStream's own methods follow.
const (
	spsSetBaseStream = 15
	spsGetBaseStream = 16
	spsBindToFile    = 17
	spsClose         = 18
)

// SPFILEMODE values for ISpStream::BindToFile.
const (
	spfmOpenReadOnly = 1
)

// ISpObjectToken : ISpDataKey : IUnknown. ISpDataKey contributes 3..14.
const (
	dkGetStringValue  = 6
	tokGetId          = 16
	tokCreateInstance = 18
)

// ISpObjectTokenCategory : ISpDataKey : IUnknown.
const (
	tcSetId      = 15
	tcGetId      = 16
	tcGetDataKey = 17
	tcEnumTokens = 18
)

// IEnumSpObjectTokens : IUnknown.
const (
	enumNext     = 3
	enumGetCount = 8
	enumItem     = 7
)

// ---- SAPI constants --------------------------------------------------------

// SPEVENTENUM values used here.
const (
	speiRecognition      = 38
	speiHypothesis       = 39
	speiFalseRecognition = 43
)

// SPRULESTATE.
const (
	sprsInactive = 0
	sprsActive   = 1
)

// SPRECOSTATE.
const (
	sprstInactive          = 0
	sprstActive            = 1
	sprstActiveAlways      = 2
	sprstInactiveWithPurge = 3
)

// SPCFGRULEATTRIBUTES.
const (
	sprafTopLevel = 0x01
	sprafActive   = 0x02
	sprafDynamic  = 0x20
	sprafRoot     = 0x40
)

// SPLOADOPTIONS.
const (
	sploStatic  = 0
	sploDynamic = 1
)

// SPWORDPRONOUNCEABLE.
const spwpKnownWordPronounceable = 1

// SPGRAMMARWORDTYPE / misc.
const spruleNoChange = 0

// ---- call helpers ----------------------------------------------------------

// comIface is the memory layout every COM object starts with: one pointer to a
// vtable, whose slot i is method i. Modelling it as a Go struct means an
// interface pointer is an ordinary *comIface, so no foreign pointer is ever
// converted back from an integer.
type comIface struct {
	vtbl *[64]uintptr
}

// comObject is an interface pointer.
type comObject = *comIface

// up passes a pointer to a COM method.
//
// The pointed-to memory must stay alive for the duration of the call. For Go
// locals that outlive the call that is automatic; for temporary buffers the
// call sites use runtime.KeepAlive. Go's collector does not move heap objects,
// which is what makes this safe in practice as well as in principle.
func up[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

// obj passes an interface pointer as a method argument.
func obj(o comObject) uintptr { return uintptr(unsafe.Pointer(o)) }

// call invokes method slot idx with this pointer prepended.
func (o comObject) call(name string, idx int, args ...uintptr) error {
	hr := o.raw(idx, args...)
	if int32(hr) < 0 {
		return &hresultError{call: name, hr: hr}
	}
	return nil
}

// raw invokes a method and returns the HRESULT without interpreting it, for the
// calls whose S_FALSE result is meaningful.
func (o comObject) raw(idx int, args ...uintptr) uintptr {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, up(o))
	all = append(all, args...)
	hr, _, _ := syscall.SyscallN(o.vtbl[idx], all...)
	return hr
}

func (o comObject) release() {
	if o != nil {
		o.raw(unkRelease)
	}
}

// coInitialize prepares the calling goroutine's thread for COM. Every SAPI call
// must happen on a thread that has done this, which is why the recogniser locks
// its worker goroutine to its OS thread.
func coInitialize() error {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(hr) < 0 {
		return &hresultError{call: "CoInitializeEx", hr: hr}
	}
	return nil
}

func coUninitialize() { procCoUninitialize.Call() }

func coCreateInstance(clsid, iid *guid) (comObject, error) {
	var p comObject
	hr, _, _ := procCoCreateInstance.Call(
		up(clsid), 0, clsctxInprocServer, up(iid), up(&p))
	if int32(hr) < 0 {
		return nil, &hresultError{call: "CoCreateInstance", hr: hr}
	}
	return p, nil
}

// coTaskMemFree releases memory a COM method allocated for us. Every out
// parameter documented as "CoMem" has to go through here or the process leaks
// a little on every recognition.
func coTaskMemFree(p unsafe.Pointer) {
	if p != nil {
		procCoTaskMemFree.Call(uintptr(p))
	}
}

// utf16 converts s for a COM call. The returned slice is the backing store and
// must be kept alive with runtime.KeepAlive until the call returns.
func utf16(s string) (*uint16, []uint16) {
	if s == "" {
		return nil, nil
	}
	b, err := syscall.UTF16FromString(s)
	if err != nil {
		return nil, nil
	}
	return &b[0], b
}

// fromUTF16 copies a NUL terminated UTF-16 string out of foreign memory.
func fromUTF16(p *uint16) string {
	if p == nil {
		return ""
	}
	const limit = 1 << 16
	n := 0
	for n < limit && *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// ---- SAPI structures -------------------------------------------------------

// spEvent mirrors SPEVENT. The two 16 bit bitfields share the first DWORD.
type spEvent struct {
	eEventID             uint16
	elParamType          uint16
	ulStreamNum          uint32
	ullAudioStreamOffset uint64
	wParam               uintptr
	// lParam is an interface pointer when elParamType says so: for
	// SPEI_RECOGNITION it is the ISpRecoResult this backend must Release.
	lParam unsafe.Pointer
}

// spPhraseRule mirrors SPPHRASERULE: the rule that matched, with the engine's
// confidence in it.
type spPhraseRule struct {
	pszName            *uint16
	ulID               uint32
	ulFirstElement     uint32
	ulCountOfElements  uint32
	_                  uint32
	pNextSibling       unsafe.Pointer
	pFirstChild        unsafe.Pointer
	sREngineConfidence float32
	confidence         int8
	_                  [3]byte
}

// spPhraseProperty mirrors SPPHRASEPROPERTY, the semantic tags declared by
// <tag> elements in the grammar. Properties form a tree: siblings are
// alternatives at the same level, children are nested rules.
type spPhraseProperty struct {
	pszName            *uint16
	ulID               uint32
	_                  uint32
	pszValue           *uint16
	vValue             [16]byte // VARIANT, read only via pszValue here
	ulFirstElement     uint32
	ulCountOfElements  uint32
	pNextSibling       *spPhraseProperty
	pFirstChild        *spPhraseProperty
	sREngineConfidence float32
	confidence         int8
	_                  [3]byte
}

// spPhrase mirrors SPPHRASE up to the fields this backend reads.
type spPhrase struct {
	cbSize                    uint32
	langID                    uint16
	wHomophoneGroupID         uint16
	ullGrammarID              uint64
	ftStartTime               uint64
	ullAudioStreamPosition    uint64
	ulAudioSizeBytes          uint32
	ulRetainedSizeBytes       uint32
	ulAudioSizeTime           uint32
	_                         uint32
	rule                      spPhraseRule
	pProperties               *spPhraseProperty
	pElements                 unsafe.Pointer
	cReplacements             uint32
	_                         uint32
	pReplacements             unsafe.Pointer
	srEngineID                guid
	ulSREnginePrivateDataSize uint32
	_                         uint32
	pSREnginePrivateData      unsafe.Pointer
}

// spPropertyInfo mirrors SPPROPERTYINFO, used to attach a semantic value to a
// word added to the dynamic callsign rule.
type spPropertyInfo struct {
	pszName  *uint16
	ulID     uint32
	_        uint32
	pszValue *uint16
	vValue   [16]byte
}
