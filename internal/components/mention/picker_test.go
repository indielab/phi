package mention

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/components"
)

func TestPickerAccept(t *testing.T) {
	var got string
	p := &Picker{
		Items: []Item{{Path: "go.mod"}, {Path: "a/b.go"}},
		OnAccept: func(item Item) {
			got = item.Path
		},
	}
	p.Show()
	p.Selected = 1
	require.True(t, p.Accept(), "accept failed")
	require.Equal(t, "a/b.go", got)
	require.False(t, p.Open, "should be closed")
}

func TestPickerHandleNav(t *testing.T) {
	p := &Picker{
		Items: []Item{{Path: "a"}, {Path: "b"}, {Path: "c"}},
	}
	p.Show()
	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyDown}), "expected consume")
	require.Equal(t, 1, p.Selected)
	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyEscape}), "expected consume")
	require.False(t, p.Open, "should close on escape")
}

func TestPickerDrawClosed(t *testing.T) {
	p := &Picker{Theme: components.DefaultTheme()}
	surf := p.Draw(components.DrawContext{
		Max:    components.Size{Width: 80, Height: 24},
		Method: xui.WidthUnicode,
	})
	require.Empty(t, surf.Children, "closed picker should have no children")
}

func TestPickerDrawOpen(t *testing.T) {
	p := &Picker{
		Theme:         components.DefaultTheme(),
		Items:         []Item{{Path: "go.mod"}, {Path: "internal/x.go"}},
		AnchorBottomY: 20,
		AnchorWidth:   60,
		AnchorX:       0,
	}
	p.Show()
	surf := p.Draw(components.DrawContext{
		Max:    components.Size{Width: 80, Height: 24},
		Method: xui.WidthUnicode,
	})
	require.Len(t, surf.Children, 1)
	child := surf.Children[0]
	require.LessOrEqual(t, child.Origin.Y+child.Surface.Size.Height, 20,
		"panel should sit above anchor: oy=%d h=%d", child.Origin.Y, child.Surface.Size.Height)
}
