package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestBuildUIMReadinessReadyWithActiveSlotAndIdentity(t *testing.T) {
	slot := &qmi.UIMSlotStatus{Slots: []qmi.UIMSlotStatusSlot{{
		PhysicalCardStatus: qmi.UIMPhysicalCardStatePresent,
		PhysicalSlotStatus: qmi.UIMSlotStateActive,
		LogicalSlot:        1,
		ICCID:              "8985203103011907194",
	}}}
	ids := DeviceIdentities{ICCID: "8985203103011907194", IMSI: "460011234567890"}

	got := buildUIMReadiness(qmi.SIMReady, &qmi.CardStatusDetails{CardState: 0x01}, slot, ids, nil)

	if !got.TransportReady || !got.ControlReady || !got.UIMReady || !got.CardPresent {
		t.Fatalf("readiness flags = %+v, want all ready", got)
	}
	if got.Reason != UIMReadinessReady {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessReady)
	}
	if !got.SlotKnown || got.ActiveSlot != 1 {
		t.Fatalf("slot known=%v slot=%d, want slot 1", got.SlotKnown, got.ActiveSlot)
	}
}

func TestBuildUIMReadinessBlockedIsNotTransportFatal(t *testing.T) {
	got := buildUIMReadiness(qmi.SIMBlocked, &qmi.CardStatusDetails{CardState: 0x02}, nil, DeviceIdentities{}, nil)

	if got.Reason != UIMReadinessSIMBlocked {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessSIMBlocked)
	}
	if !got.TransportReady || !got.ControlReady {
		t.Fatalf("blocked SIM should keep transport/control ready: %+v", got)
	}
}

func TestBuildUIMReadinessPINRequiredIsActionableNotResetting(t *testing.T) {
	got := buildUIMReadiness(qmi.SIMPINRequired, &qmi.CardStatusDetails{CardState: 0x01}, nil, DeviceIdentities{}, nil)

	if got.Reason != UIMReadinessSIMBlocked {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessSIMBlocked)
	}
	if !got.TransportReady || !got.ControlReady {
		t.Fatalf("pin required should keep transport/control ready: %+v", got)
	}
}

func TestBuildUIMReadinessTransportFatalFromDeviceOpenError(t *testing.T) {
	err := errors.New("failed to open qmi device /dev/cdc-wdm1: no such device")
	got := buildUIMReadiness(qmi.SIMNotReady, nil, nil, DeviceIdentities{}, err)

	if got.Reason != UIMReadinessTransportFatal {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessTransportFatal)
	}
	if got.TransportReady {
		t.Fatalf("TransportReady=true for fatal transport error: %+v", got)
	}
}

func TestBuildUIMReadinessControlUnavailableForTimeout(t *testing.T) {
	err := errors.New("UIM GetCardStatus: context deadline exceeded")
	got := buildUIMReadiness(qmi.SIMNotReady, nil, nil, DeviceIdentities{}, err)

	if got.Reason != UIMReadinessControlUnavailable {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessControlUnavailable)
	}
	if !got.TransportReady || got.ControlReady {
		t.Fatalf("timeout should mean transport ready but control unavailable: %+v", got)
	}
}

func TestBuildUIMReadinessIgnoresNonFatalSlotStatusErrorWhenIdentityReady(t *testing.T) {
	ids := DeviceIdentities{ICCID: "8985203103011907194", IMSI: "460011234567890"}

	got := buildUIMReadinessWithSlotError(qmi.SIMReady, &qmi.CardStatusDetails{CardState: 0x01}, nil, ids, nil, errors.New("QMI error: service=0x0b msg=0x0047 result=0x0001 error=0x0034"))

	if got.Reason != UIMReadinessReady {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessReady)
	}
	if got.SlotKnown {
		t.Fatalf("SlotKnown=true, want false when slot status failed nonfatally: %+v", got)
	}
}

func TestBuildUIMReadinessPromotesFatalSlotStatusError(t *testing.T) {
	ids := DeviceIdentities{ICCID: "8985203103011907194", IMSI: "460011234567890"}

	got := buildUIMReadinessWithSlotError(qmi.SIMReady, &qmi.CardStatusDetails{CardState: 0x01}, nil, ids, nil, errors.New("failed to open qmi device /dev/cdc-wdm1: no such device"))

	if got.Reason != UIMReadinessTransportFatal {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessTransportFatal)
	}
	if got.TransportReady || got.ControlReady {
		t.Fatalf("fatal slot error should mark transport/control not ready: %+v", got)
	}
}

func TestResolveActiveUIMSlotPrefersActivePresentSlot(t *testing.T) {
	info := &qmi.UIMSlotStatus{Slots: []qmi.UIMSlotStatusSlot{
		{PhysicalCardStatus: qmi.UIMPhysicalCardStateAbsent, PhysicalSlotStatus: qmi.UIMSlotStateInactive, LogicalSlot: 1},
		{PhysicalCardStatus: qmi.UIMPhysicalCardStatePresent, PhysicalSlotStatus: qmi.UIMSlotStateActive, LogicalSlot: 2, ICCID: "8985"},
	}}

	slot, ok, source := resolveActiveUIMSlot(info)

	if !ok || slot != 2 || source != "uim_slot_status" {
		t.Fatalf("slot=%d ok=%v source=%q, want slot 2 from uim_slot_status", slot, ok, source)
	}
}

func TestGetUIMReadinessUsesUIMRecoveryWrapperForCardStatus(t *testing.T) {
	wantErr := errors.New("lazy allocation failed")
	var calls int
	m := &Manager{}
	m.ensureUIMServiceHook = func() (*qmi.UIMService, error) {
		calls++
		return nil, wantErr
	}

	got, err := m.GetUIMReadiness(context.Background())

	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("ensure UIM calls=%d, want 1", calls)
	}
	if got.Reason != UIMReadinessControlUnavailable {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessControlUnavailable)
	}
}
