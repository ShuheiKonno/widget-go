package sysinfo

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

// PowerModeInfo は /api/system に載せる電源モードの状態。
// 取得できない環境（API が無い、読み取りに失敗）では GetPowerMode が nil を返し、UI は行ごと隠す。
type PowerModeInfo struct {
	// PlanOK は電源プランがバランスで、電源モードを切り替えられること。
	PlanOK bool `json:"planOK"`
	// Active は強調するモード。PowerModeSaver / PowerModeBalanced / PowerModeBest / PowerModeOther。
	Active string `json:"active"`
}

var (
	// ErrPowerModeInvalid は許可リスト外の mode が指定されたことを表す。
	ErrPowerModeInvalid = errors.New("不正な電源モードです")
	// ErrPowerPlanNotBalanced は電源プランがバランス以外で、電源モードが効かないことを表す。
	ErrPowerPlanNotBalanced = errors.New("電源プランが「バランス」以外のため切り替えできません")
)

// powerSetMu は SetPowerMode の「電源プランの確認 → 設定」を直列化する。
var powerSetMu sync.Mutex

// GetPowerMode は現在の電源モードの状態を返す。取得できなければ nil。
func GetPowerMode() *PowerModeInfo {
	return getPowerMode(ActivePowerScheme, EffectiveOverlay)
}

func getPowerMode(scheme func() (windows.GUID, error), overlay func() (windows.GUID, error)) *PowerModeInfo {
	s, err := scheme()
	if err != nil {
		return nil
	}
	o, err := overlay()
	if err != nil {
		return nil
	}
	active, planOK := ResolvePowerMode(s, o)
	return &PowerModeInfo{PlanOK: planOK, Active: active}
}

// SetPowerMode は電源モードを mode（PowerModeSaver / PowerModeBalanced / PowerModeBest）に切り替える。
// 電源プランは変更しない。バランス以外のプランが有効なときは何も設定せず ErrPowerPlanNotBalanced を返す。
// mode が許可リスト外なら ErrPowerModeInvalid、API が使えなければ ErrPowerModeUnsupported を返す。
func SetPowerMode(mode string) error {
	return setPowerMode(mode, ActivePowerScheme, SetOverlay)
}

func setPowerMode(mode string, scheme func() (windows.GUID, error), setOverlay func(windows.GUID) error) error {
	g, ok := OverlayForMode(mode)
	if !ok {
		return fmt.Errorf("%w: %q", ErrPowerModeInvalid, mode)
	}
	powerSetMu.Lock()
	defer powerSetMu.Unlock()
	s, err := scheme()
	if err != nil {
		return err
	}
	if s != PowerSchemeBalanced {
		return ErrPowerPlanNotBalanced
	}
	return setOverlay(g)
}
