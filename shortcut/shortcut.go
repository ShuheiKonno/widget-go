// Package shortcut は、スタートメニューとスタートアップに置く .lnk（ショートカット）の作成・削除・確認を行う。
//
// 設定画面のチェックと対応する。チェックを入れるとショートカットを保存し、外すと削除する。
// 状態はファイルの有無で決める（config.json には持たない）。同じ名前のファイルは、作った経緯によらず
// 利用者の登録とみなして扱う。
package shortcut

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

// Kind はショートカットの置き場所。
type Kind string

const (
	StartMenu Kind = "startmenu" // スタートメニューのプログラム一覧
	Startup   Kind = "startup"   // スタートアップ（ログイン時に自動起動）
)

// FileName はショートカットのファイル名（実行ファイル名 system-widget.exe に合わせた固定名）。
// スタートメニューとスタートアップのどちらも、同じ名前で 1 つだけ作る。
const FileName = "system-widget.lnk"

// Description はショートカットの説明（スタートメニューのツールチップなど）。
const Description = "システムモニター"

// Locations はショートカットを置くフォルダ。テストで差し替えられるよう、構造体にしている。
type Locations struct {
	Programs string // スタートメニューのプログラム一覧
	Startup  string // スタートアップ
}

// DefaultLocations は現在の利用者の既知フォルダを返す。フォルダのリダイレクト
// （OneDrive や企業の設定による移動）に対応するため、%APPDATA% から組み立てない。
func DefaultLocations() (Locations, error) {
	programs, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return Locations{}, fmt.Errorf("スタートメニューのフォルダを取得できません: %w", err)
	}
	startup, err := windows.KnownFolderPath(windows.FOLDERID_Startup, 0)
	if err != nil {
		return Locations{}, fmt.Errorf("スタートアップのフォルダを取得できません: %w", err)
	}
	return Locations{Programs: programs, Startup: startup}, nil
}

// Path は kind のショートカットのフルパス。
func (l Locations) Path(kind Kind) (string, error) {
	switch kind {
	case StartMenu:
		return filepath.Join(l.Programs, FileName), nil
	case Startup:
		return filepath.Join(l.Startup, FileName), nil
	}
	return "", fmt.Errorf("不明な種別です: %q", kind)
}

// State はショートカットの現在の状態。
type State struct {
	Exists bool   `json:"exists"`
	Target string `json:"target,omitempty"` // 現在のリンク先
	// Mismatch は、ショートカットはあるが、リンク先が今動いている実行ファイルと違うこと。
	// 古い場所を指していたり、検証用のビルドを指していたりするので、設定画面に警告だけを出す
	// （利用者の登録を勝手に書き換えない）。
	Mismatch bool `json:"mismatch,omitempty"`
}

// Status は kind のショートカットの状態を返す。exe は今動いている実行ファイルのパス。
func Status(l Locations, kind Kind, exe string) (State, error) {
	p, err := l.Path(kind)
	if err != nil {
		return State{}, err
	}
	if _, err := os.Stat(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, err
	}
	target, err := readTarget(p)
	if err != nil {
		// 読めなくても、存在はしている。壊れたショートカットとして警告扱いにする。
		return State{Exists: true, Mismatch: true}, nil
	}
	return State{Exists: true, Target: target, Mismatch: !samePath(target, exe)}, nil
}

// Enable は kind のショートカットを作る（同じ名前があれば、リンク先を exe にして上書きする）。
func Enable(l Locations, kind Kind, exe string) error {
	p, err := l.Path(kind)
	if err != nil {
		return err
	}
	if exe == "" {
		return errors.New("実行ファイルのパスが空です")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return writeShortcut(p, exe)
}

// Disable は kind のショートカットを削除する。無ければ何もしない。
// 削除するのは FileName のファイルだけで、他の名前のファイルには触れない。
func Disable(l Locations, kind Kind) error {
	p, err := l.Path(kind)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// withCOM は専用のスレッドで COM を初期化して f を実行する。WebView2 が UI スレッドで COM を
// STA として初期化済みなので、UI スレッドでは呼ばないこと（ここで新しい goroutine に固定する）。
func withCOM(f func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
			// S_FALSE（既に初期化済み）は成功扱い。それ以外はエラー。
			var oe *ole.OleError
			if !errors.As(err, &oe) || oe.Code() != 1 {
				done <- fmt.Errorf("COM を初期化できません: %w", err)
				return
			}
		}
		defer ole.CoUninitialize()
		done <- f()
	}()
	return <-done
}

// withShell は WScript.Shell の IDispatch を渡して f を実行する。使い終えたら Release される。
func withShell(f func(shell *ole.IDispatch) error) error {
	return withCOM(func() error {
		unk, err := oleutil.CreateObject("WScript.Shell")
		if err != nil {
			return fmt.Errorf("WScript.Shell を作成できません: %w", err)
		}
		defer unk.Release()
		shell, err := unk.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return err
		}
		defer shell.Release()
		return f(shell)
	})
}

func writeShortcut(path, exe string) error {
	return withShell(func(shell *ole.IDispatch) error {
		v, err := oleutil.CallMethod(shell, "CreateShortcut", path)
		if err != nil {
			return fmt.Errorf("ショートカットを作成できません: %w", err)
		}
		link := v.ToIDispatch()
		defer link.Release()
		props := []struct {
			name string
			val  string
		}{
			{"TargetPath", exe},
			{"WorkingDirectory", filepath.Dir(exe)},
			{"Description", Description},
			{"IconLocation", exe + ",0"},
		}
		for _, p := range props {
			if _, err := oleutil.PutProperty(link, p.name, p.val); err != nil {
				return fmt.Errorf("%s を設定できません: %w", p.name, err)
			}
		}
		if _, err := oleutil.CallMethod(link, "Save"); err != nil {
			return fmt.Errorf("ショートカットを保存できません: %w", err)
		}
		return nil
	})
}

func readTarget(path string) (string, error) {
	var target string
	err := withShell(func(shell *ole.IDispatch) error {
		v, err := oleutil.CallMethod(shell, "CreateShortcut", path)
		if err != nil {
			return err
		}
		link := v.ToIDispatch()
		defer link.Release()
		t, err := oleutil.GetProperty(link, "TargetPath")
		if err != nil {
			return err
		}
		target = t.ToString()
		return nil
	})
	return target, err
}
