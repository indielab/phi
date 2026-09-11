package splash

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/components"
)

func TestScreenDrawLayout(t *testing.T) {
	w := Screen{
		Sphere: &Sphere{Time: 1},
		Theme:  components.DefaultTheme(),
		Brand:  "phi",
	}
	surf := w.Draw(components.DrawContext{
		Max:    components.Size{Width: 100, Height: 40},
		Method: xui.WidthUnicode,
	})
	require.Len(t, surf.Children, 2, "sphere + text")
}
