//go:build windows

package sapi

import (
	"testing"
	"unsafe"
)

// The SAPI structures are read straight out of memory the engine allocated, so
// a single wrong field offset silently yields garbage confidence values or a
// walk off the end of the property list. These are the offsets from sapi.h on
// x64; CI runs them on windows-latest, which is the only place they can be
// wrong and the only place it matters.
func TestStructLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout constants are for x64")
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"SPEVENT.ulStreamNum", unsafe.Offsetof(spEvent{}.ulStreamNum), 4},
		{"SPEVENT.ullAudioStreamOffset", unsafe.Offsetof(spEvent{}.ullAudioStreamOffset), 8},
		{"SPEVENT.wParam", unsafe.Offsetof(spEvent{}.wParam), 16},
		{"SPEVENT.lParam", unsafe.Offsetof(spEvent{}.lParam), 24},
		{"sizeof(SPEVENT)", unsafe.Sizeof(spEvent{}), 32},

		{"SPPHRASERULE.pNextSibling", unsafe.Offsetof(spPhraseRule{}.pNextSibling), 24},
		{"SPPHRASERULE.SREngineConfidence", unsafe.Offsetof(spPhraseRule{}.sREngineConfidence), 40},
		{"sizeof(SPPHRASERULE)", unsafe.Sizeof(spPhraseRule{}), 48},

		{"SPPHRASEPROPERTY.pszValue", unsafe.Offsetof(spPhraseProperty{}.pszValue), 16},
		{"SPPHRASEPROPERTY.vValue", unsafe.Offsetof(spPhraseProperty{}.vValue), 24},
		{"SPPHRASEPROPERTY.pNextSibling", unsafe.Offsetof(spPhraseProperty{}.pNextSibling), 48},
		{"SPPHRASEPROPERTY.pFirstChild", unsafe.Offsetof(spPhraseProperty{}.pFirstChild), 56},
		{"sizeof(SPPHRASEPROPERTY)", unsafe.Sizeof(spPhraseProperty{}), 72},

		{"SPPHRASE.Rule", unsafe.Offsetof(spPhrase{}.rule), 48},
		{"SPPHRASE.pProperties", unsafe.Offsetof(spPhrase{}.pProperties), 96},

		{"SPPROPERTYINFO.pszValue", unsafe.Offsetof(spPropertyInfo{}.pszValue), 16},
		{"sizeof(SPPROPERTYINFO)", unsafe.Sizeof(spPropertyInfo{}), 40},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// The GUID literals are parsed at init; a typo would panic at first use rather
// than at start up, so pin them here.
func TestGUIDs(t *testing.T) {
	if clsidSpInprocRecognizer.Data1 != 0x41B89B6B {
		t.Errorf("CLSID_SpInprocRecognizer = %+v", clsidSpInprocRecognizer)
	}
	if iidISpRecognizer.Data1 != 0xC2B5F241 {
		t.Errorf("IID_ISpRecognizer = %+v", iidISpRecognizer)
	}
}
