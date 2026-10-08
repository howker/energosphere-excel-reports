//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/howker/energosphere-excel-reports/internal/batch"
	"github.com/howker/energosphere-excel-reports/internal/importer"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/report"
)

var user = syscall.NewLazyDLL("user32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var comctl = syscall.NewLazyDLL("comctl32.dll")
var comdlg = syscall.NewLazyDLL("comdlg32.dll")
var shell = syscall.NewLazyDLL("shell32.dll")
var ole = syscall.NewLazyDLL("ole32.dll")
var gdi = syscall.NewLazyDLL("gdi32.dll")

func u(s string) *uint16    { return syscall.StringToUTF16Ptr(s) }
func ptr(p *uint16) *uint16 { return p }

// Retain typed Go pointers throughout a synchronous native call. Converting
// strings to uintptr in the caller would let GC reclaim them prematurely.
func call(d *syscall.LazyDLL, name string, args ...interface{}) uintptr {
	values := make([]uintptr, len(args))
	for i, arg := range args {
		switch a := arg.(type) {
		case uintptr:
			values[i] = a
		case int:
			values[i] = uintptr(a)
		case uint32:
			values[i] = uintptr(a)
		case *uint16:
			values[i] = uintptr(unsafe.Pointer(a))
		case unsafe.Pointer:
			values[i] = uintptr(a)
		default:
			panic(fmt.Sprintf("unsupported native argument %T", arg))
		}
	}
	v, _, _ := d.NewProc(name).Call(values...)
	runtime.KeepAlive(args)
	return v
}
func send(h uintptr, m uint32, w uintptr, l interface{}) uintptr {
	return call(user, "SendMessageW", h, uintptr(m), w, l)
}
func setText(h uintptr, s string) { call(user, "SetWindowTextW", h, ptr(u(s))) }
func getText(h uintptr) string {
	n := call(user, "GetWindowTextLengthW", h) + 1
	b := make([]uint16, n)
	call(user, "GetWindowTextW", h, unsafe.Pointer(&b[0]), n)
	return syscall.UTF16ToString(b)
}
func showError(s string) {
	call(user, "MessageBoxW", 0, ptr(u(s)), ptr(u("Паспорта — ошибка")), 0x10)
}

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type wndClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type lvItem struct {
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
	Indent, GroupID  int32
	Columns          uint32
	Cols, ColFormats uintptr
	Group            int32
}
type lvColumn struct {
	Mask                                                               uint32
	Format, Width                                                      int32
	Text                                                               *uint16
	TextMax, SubItem, Image, Order, MinWidth, DefaultWidth, IdealWidth int32
}
type nmhdr struct {
	Hwnd, ID uintptr
	Code     uint32
}
type nmList struct {
	Header                      nmhdr
	Item, SubItem               int32
	NewState, OldState, Changed uint32
	Action                      point
	Param                       uintptr
}
type openFileName struct {
	Size                   uint32
	Owner, Instance        uintptr
	Filter, CustomFilter   *uint16
	CustomMax, FilterIndex uint32
	File                   *uint16
	FileMax                uint32
	FileTitle              *uint16
	FileTitleMax           uint32
	InitialDir, Title      *uint16
	Flags                  uint32
	FileOffset, Extension  uint16
	DefaultExt             *uint16
	CustomData, Hook       uintptr
	Template               *uint16
	Reserved               uintptr
	Reserved2, FlagsEx     uint32
}
type browseInfo struct {
	Owner, Root        uintptr
	DisplayName, Title *uint16
	Flags              uint32
	Callback, Param    uintptr
	Image              int32
}

const (
	wMEvent  = 0x8001
	idOpen   = 100
	idSearch = 101
	idObject = 102
	idList   = 103
	idAll    = 104
	idNone   = 105
	idDir    = 106
	idBrowse = 107
	idPrefix = 108
	idStart  = 109
	idCancel = 110
	idFolder = 111
)

type event struct {
	Items    []passport.Passport
	Err      error
	Progress *batch.Progress
	Result   *batch.Result
	Source   string
}
type appState struct {
	window                             uintptr
	controls                           map[int]uintptr
	status, preview, progress, summary uintptr
	items                              []passport.Passport
	visible                            []int
	selected                           map[int]bool
	events                             chan event
	busy, updating                     bool
	cancel                             context.CancelFunc
	font                               uintptr
}

var app *appState

func (a *appState) control(class, text string, id, x, y, w, h int, style uint32) uintptr {
	hwnd := call(user, "CreateWindowExW", 0, ptr(u(class)), ptr(u(text)), uintptr(0x50000000|style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), a.window, uintptr(id), call(kernel, "GetModuleHandleW", 0), 0)
	if id != 0 {
		a.controls[id] = hwnd
	}
	send(hwnd, 0x30, a.font, 1)
	return hwnd
}
func (a *appState) busyControls(b bool) {
	a.busy = b
	enable := uintptr(1)
	if b {
		enable = 0
	}
	for id, h := range a.controls {
		if id == idCancel {
			continue
		}
		call(user, "EnableWindow", h, enable)
	}
	e := uintptr(0)
	if b && a.cancel != nil {
		e = 1
	}
	call(user, "EnableWindow", a.controls[idCancel], e)
}
func (a *appState) emit(e event) { a.events <- e; call(user, "PostMessageW", a.window, wMEvent, 0, 0) }
func (a *appState) rebuild() {
	a.updating = true
	defer func() { a.updating = false }()
	send(a.controls[idList], 0x1009, 0, 0)
	a.visible = nil
	query := strings.ToLower(strings.TrimSpace(getText(a.controls[idSearch])))
	object := getText(a.controls[idObject])
	if object == "Все объекты" {
		object = ""
	}
	for i, p := range a.items {
		if object != "" && object != p.Object {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(p.Object+" "+p.Connection+" "+p.Meter.Serial), query) {
			continue
		}
		row := len(a.visible)
		a.visible = append(a.visible, i)
		t := u(p.Object)
		item := lvItem{Mask: 1, Item: int32(row), Text: t}
		send(a.controls[idList], 0x104d, 0, unsafe.Pointer(&item))
		for j, text := range []string{p.Connection, p.Meter.Serial} {
			item.SubItem = int32(j + 1)
			item.Text = u(text)
			send(a.controls[idList], 0x104c, 0, unsafe.Pointer(&item))
		}
		state := uint32(1 << 12)
		if a.selected[i] {
			state = 2 << 12
		}
		item = lvItem{State: state, StateMask: 0xf000}
		send(a.controls[idList], 0x102b, uintptr(row), unsafe.Pointer(&item))
	}
	a.updateSelection()
}
func (a *appState) updateSelection() {
	n := 0
	for _, b := range a.selected {
		if b {
			n++
		}
	}
	setText(a.summary, fmt.Sprintf("Всего: %d    В списке: %d    Выбрано: %d", len(a.items), len(a.visible), n))
	preview := "Имя файла: выберите присоединение"
	for _, i := range a.visible {
		if a.selected[i] {
			preview = "Имя файла: " + report.Filename(a.items[i], getText(a.controls[idPrefix]))
			break
		}
	}
	setText(a.preview, preview)
}
func (a *appState) load() {
	buf := make([]uint16, 32768)
	filters := append(append(syscall.StringToUTF16("Excel (*.xlsx)"), syscall.StringToUTF16("*.xlsx")...), 0)
	of := openFileName{Owner: a.window, Filter: &filters[0], FilterIndex: 1, File: &buf[0], FileMax: uint32(len(buf)), Title: u("Выберите исходный Excel"), Flags: 0x80000 | 0x1000 | 0x800 | 0x8}
	of.Size = uint32(unsafe.Sizeof(of))
	if call(comdlg, "GetOpenFileNameW", unsafe.Pointer(&of)) == 0 {
		return
	}
	filename := syscall.UTF16ToString(buf)
	a.busyControls(true)
	setText(a.status, "Чтение Excel…")
	go func() { items, err := importer.Load(filename); a.emit(event{Items: items, Err: err, Source: filename}) }()
}
func (a *appState) folder() {
	buf := make([]uint16, 260)
	bi := browseInfo{Owner: a.window, DisplayName: &buf[0], Title: u("Выберите папку паспортов"), Flags: 0x41}
	pid := call(shell, "SHBrowseForFolderW", unsafe.Pointer(&bi))
	if pid == 0 {
		return
	}
	defer call(ole, "CoTaskMemFree", pid)
	if call(shell, "SHGetPathFromIDListW", pid, unsafe.Pointer(&buf[0])) != 0 {
		setText(a.controls[idDir], syscall.UTF16ToString(buf))
	}
}
func (a *appState) start() {
	var picked []passport.Passport
	for i, p := range a.items {
		if a.selected[i] {
			picked = append(picked, p)
		}
	}
	if len(picked) == 0 {
		showError("Выберите хотя бы одно присоединение.")
		return
	}
	dir := strings.TrimSpace(getText(a.controls[idDir]))
	if dir == "" {
		showError("Укажите папку паспортов.")
		return
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		showError(err.Error())
		return
	}
	setText(a.controls[idDir], dir)
	prefix := getText(a.controls[idPrefix])
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.busyControls(true)
	send(a.progress, 0x402, 0, 0)
	setText(a.status, "Начало формирования…")
	go func() {
		r := batch.Run(ctx, report.Template, picked, dir, prefix, func(p batch.Progress) { a.emit(event{Progress: &p}) })
		a.emit(event{Result: &r})
	}()
}
func (a *appState) handleEvents() {
	for {
		select {
		case e := <-a.events:
			if e.Progress != nil {
				p := e.Progress
				setText(a.status, fmt.Sprintf("%d / %d · готово %d · %s", p.Done, p.Total, p.Success, p.Current))
				if p.Total > 0 {
					send(a.progress, 0x402, uintptr(p.Done*100/p.Total), 0)
				}
				continue
			}
			if e.Result != nil {
				r := e.Result
				if a.cancel != nil {
					a.cancel()
					a.cancel = nil
				}
				a.busyControls(false)
				text := fmt.Sprintf("Готово: %d. Ошибок: %d.", len(r.Files), len(r.Errors))
				if r.Cancelled {
					text = "Отменено. " + text
				}
				setText(a.status, text)
				if len(r.Errors) > 0 {
					showError(text + "\n\n" + strings.Join(r.Errors, "\n"))
				}
				continue
			}
			a.busyControls(false)
			if e.Err != nil {
				setText(a.status, "Ошибка загрузки")
				showError(e.Err.Error())
				continue
			}
			a.items = e.Items
			a.selected = map[int]bool{}
			objects := map[string]bool{}
			for i, p := range a.items {
				a.selected[i] = true
				objects[p.Object] = true
			}
			a.updating = true
			send(a.controls[idObject], 0x14b, 0, 0)
			send(a.controls[idObject], 0x143, 0, ptr(u("Все объекты")))
			var names []string
			for n := range objects {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				send(a.controls[idObject], 0x143, 0, ptr(u(n)))
			}
			send(a.controls[idObject], 0x14e, 0, 0)
			setText(a.controls[idSearch], "")
			a.updating = false
			a.rebuild()
			setText(a.status, "Загружено: "+e.Source)
		default:
			return
		}
	}
}
func windowProc(hwnd uintptr, m uint32, w uintptr, l unsafe.Pointer) uintptr {
	if app == nil {
		return call(user, "DefWindowProcW", hwnd, uintptr(m), w, l)
	}
	switch m {
	case 0x10: // Keep a live worker's window until it has delivered its result.
		if app.busy {
			if app.cancel != nil {
				app.cancel()
				setText(app.status, "Отмена… дождитесь завершения текущего файла.")
			}
			return 0
		}
		call(user, "DestroyWindow", hwnd)
		return 0
	case 2:
		call(user, "PostQuitMessage", 0)
		return 0
	case wMEvent:
		app.handleEvents()
		return 0
	case 0x111:
		id := int(w & 0xffff)
		notification := int(w >> 16)
		if app.updating {
			return 0
		}
		if id == idSearch && notification == 0x300 {
			app.rebuild()
			return 0
		}
		if id == idPrefix && notification == 0x300 {
			app.updateSelection()
			return 0
		}
		if id == idObject && notification == 1 {
			app.rebuild()
			return 0
		}
		if app.busy && id != idCancel {
			return 0
		}
		switch id {
		case idOpen:
			app.load()
		case idAll:
			for _, i := range app.visible {
				app.selected[i] = true
			}
			app.rebuild()
		case idNone:
			for _, i := range app.visible {
				delete(app.selected, i)
			}
			app.rebuild()
		case idBrowse:
			app.folder()
		case idStart:
			app.start()
		case idCancel:
			if app.cancel != nil {
				app.cancel()
				setText(app.status, "Отмена после текущего паспорта…")
			}
		case idFolder:
			dir := getText(app.controls[idDir])
			call(shell, "ShellExecuteW", hwnd, ptr(u("open")), ptr(u(dir)), 0, 0, 1)
		}
		return 0
	case 0x4e:
		n := (*nmList)(l)
		if n.Header.ID == idList && n.Header.Code == ^uint32(100) && !app.updating && n.Item >= 0 && int(n.Item) < len(app.visible) && n.Changed&8 != 0 && (n.NewState&0xf000) != (n.OldState&0xf000) {
			app.selected[app.visible[n.Item]] = n.NewState&0xf000 == 2<<12
			app.updateSelection()
		}
		return 0
	}
	return call(user, "DefWindowProcW", hwnd, uintptr(m), w, l)
}
func runGUI(initialSource, initialDir, initialPrefix string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	call(ole, "OleInitialize", 0)
	defer call(ole, "OleUninitialize")
	call(user, "SetProcessDPIAware")
	init := struct{ Size, Classes uint32 }{8, 0x21}
	call(comctl, "InitCommonControlsEx", unsafe.Pointer(&init))
	instance := call(kernel, "GetModuleHandleW", 0)
	class := wndClass{Proc: syscall.NewCallback(windowProc), Instance: instance, Cursor: call(user, "LoadCursorW", 0, 32512), Background: 6, Name: u("ESReportsLegacy")}
	class.Size = uint32(unsafe.Sizeof(class))
	if call(user, "RegisterClassExW", unsafe.Pointer(&class)) == 0 {
		return fmt.Errorf("не удалось зарегистрировать окно: %v", syscall.GetLastError())
	}
	a := &appState{controls: map[int]uintptr{}, selected: map[int]bool{}, events: make(chan event, 128)}
	app = a
	a.font = call(gdi, "CreateFontW", uintptr(0xfffffff1), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, ptr(u("Segoe UI")))
	defer call(gdi, "DeleteObject", a.font)
	a.window = call(user, "CreateWindowExW", 0, ptr(class.Name), ptr(u("Паспорта присоединений — Excel")), 0x00ca0000, 100, 80, 1060, 720, 0, 0, instance, 0)
	if a.window == 0 {
		return fmt.Errorf("не удалось создать окно: %v", syscall.GetLastError())
	}
	a.control("BUTTON", "Открыть Excel…", idOpen, 16, 16, 160, 30, 0)
	a.control("STATIC", "Поиск:", 0, 190, 22, 58, 22, 0)
	a.control("EDIT", "", idSearch, 250, 17, 325, 28, 0x00810080)
	a.control("STATIC", "Объект:", 0, 592, 22, 65, 22, 0)
	a.control("COMBOBOX", "", idObject, 660, 17, 366, 330, 0x00210003)
	send(a.controls[idObject], 0x143, 0, ptr(u("Все объекты")))
	send(a.controls[idObject], 0x14e, 0, 0)
	a.control("BUTTON", "Выбрать весь список", idAll, 16, 58, 190, 28, 0)
	a.control("BUTTON", "Снять выбор в списке", idNone, 218, 58, 190, 28, 0)
	a.summary = a.control("STATIC", "Откройте исходный файл Excel", 0, 425, 64, 600, 24, 0)
	h := a.control("SysListView32", "", idList, 16, 100, 1010, 355, 0x00810001)
	send(h, 0x1036, 0, 0x10024)
	for i, c := range []struct {
		Text  string
		Width int32
	}{{"Объект", 140}, {"Присоединение", 660}, {"№ счётчика", 175}} {
		column := lvColumn{Mask: 2 | 4, Width: c.Width, Text: u(c.Text)}
		send(h, 0x1061, uintptr(i), unsafe.Pointer(&column))
	}
	a.control("STATIC", "Папка:", 0, 16, 473, 74, 24, 0)
	exe, _ := os.Executable()
	dir := filepath.Join(filepath.Dir(exe), "Паспорта")
	if initialDir != "" {
		dir = initialDir
	}
	a.control("EDIT", dir, idDir, 94, 468, 804, 28, 0x00810080)
	a.control("BUTTON", "Выбрать…", idBrowse, 912, 468, 114, 28, 0)
	a.control("STATIC", "Верхний уровень имени (необязательно):", 0, 16, 512, 340, 24, 0)
	a.control("EDIT", initialPrefix, idPrefix, 360, 507, 666, 28, 0x00810080)
	a.preview = a.control("STATIC", "Имя файла: выберите присоединение", 0, 16, 546, 1010, 30, 0)
	a.control("BUTTON", "Сформировать паспорта", idStart, 16, 586, 230, 32, 0)
	a.control("BUTTON", "Отмена", idCancel, 260, 586, 110, 32, 0)
	a.control("BUTTON", "Открыть папку", idFolder, 862, 586, 164, 32, 0)
	a.progress = a.control("msctls_progress32", "", 0, 388, 590, 455, 24, 0)
	send(a.progress, 0x406, 0, 100)
	a.status = a.control("STATIC", "Готово к загрузке. Выбор и снятие выбора действуют на текущий список.", 0, 16, 635, 1010, 28, 0)
	a.busyControls(false)
	call(user, "ShowWindow", a.window, 10) // Respect the launcher's initial show state.
	call(user, "UpdateWindow", a.window)
	if initialSource != "" {
		a.busyControls(true)
		go func() {
			items, err := importer.Load(initialSource)
			a.emit(event{Items: items, Err: err, Source: initialSource})
		}()
	}
	var message msg
	for {
		r := call(user, "GetMessageW", unsafe.Pointer(&message), 0, 0, 0)
		if r == 0 {
			break
		}
		if int32(r) == -1 {
			return fmt.Errorf("ошибка очереди сообщений")
		}
		if call(user, "IsDialogMessageW", a.window, unsafe.Pointer(&message)) == 0 {
			call(user, "TranslateMessage", unsafe.Pointer(&message))
			call(user, "DispatchMessageW", unsafe.Pointer(&message))
		}
	}
	return nil
}
