//go:build integration

package integration

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDungeonExploreReplayBackgroundPNG(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupDungeonCampaign(t, tc, "explore-replay-background")
	dir := root + "/dungeon/completed/background-fixture"
	require.NoError(t, tc.WriteFile(dir+"/fest.yaml", "metadata:\n  name: Background fixture\n  id: BG0001\n"))
	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 180, 0, 255}
	global := color.Palette{color.Black, blue, green, color.White}
	local := color.Palette{color.RGBA{}, color.RGBA{255, 0, 0, 255}}
	first := image.NewPaletted(image.Rect(0, 0, 4, 3), global)
	for i := range first.Pix {
		first.Pix[i] = 1
	}
	second := image.NewPaletted(image.Rect(1, 0, 3, 2), local)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	third := image.NewPaletted(image.Rect(3, 2, 4, 3), local)
	third.Pix[0] = 1
	fixture := &gif.GIF{Image: []*image.Paletted{first, second, third}, Delay: []int{1, 1, 1}, Disposal: []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalNone}, Config: image.Config{ColorModel: global, Width: 4, Height: 3}, BackgroundIndex: 2}
	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, fixture))
	require.NoError(t, tc.WriteFile(dir+"/replay.base64", base64.StdEncoding.EncodeToString(buf.Bytes())))
	tc.Shell(t, "base64 -d "+dir+"/replay.base64 > "+dir+"/festival-replay.gif")
	for _, protocol := range []struct{ name, marker, pattern string }{
		{"kitty", "\x1b_Ga=T", `\x1b_Ga=T[^;]*;([A-Za-z0-9+/=]+)\x1b\\`},
		{"iterm", "\x1b]1337;File=", `\x1b\]1337;File=[^:]*:([A-Za-z0-9+/=]+)\x07`},
	} {
		t.Run(protocol.name, func(t *testing.T) {
			output, err := tc.RunCampInteractiveStepsInDirWithEnv(root, map[string]string{"OBEY_REDUCED_MOTION": "1"}, []InteractiveStep{{WaitFor: protocol.marker, Input: "q"}}, "dungeon", "explore", "--images", protocol.name)
			require.NoError(t, err, "%s", output)
			// Keep the real terminal stream intact and decode its image command.
			matches := regexp.MustCompile(protocol.pattern).FindAllStringSubmatch(output, -1)
			require.NotEmpty(t, matches, "no complete PNG image command in terminal output")
			for _, match := range matches {
				payload, err := base64.StdEncoding.DecodeString(match[1])
				require.NoError(t, err)
				img, err := png.Decode(bytes.NewReader(payload))
				require.NoError(t, err)
				require.Equal(t, image.Rect(0, 0, 4, 3), img.Bounds())
				require.Equal(t, green, color.RGBAModel.Convert(img.At(1, 0)), "disposed area must use the global background")
				require.Equal(t, blue, color.RGBAModel.Convert(img.At(0, 0)), "pixels outside disposal must survive")
				require.Equal(t, color.RGBA{255, 0, 0, 255}, color.RGBAModel.Convert(img.At(3, 2)), "final frame must be painted")
			}
		})
	}
}
