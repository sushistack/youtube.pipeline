//go:build comfyui_integration
// +build comfyui_integration

// Package comfyui_test integration suite — live calls against a real ComfyUI
// 0.12.3 server. Excluded from the default `go test` run by the build tag so
// CI never depends on a local GPU.
//
// Run:
//
//	go test -tags comfyui_integration -v -timeout 20m \
//	  ./internal/llmclient/comfyui/...
//
// Prerequisites: ComfyUI must be running at http://127.0.0.1:8188 with the
// FLUX.2 Klein 4B FP8 distilled model installed. The test self-skips when the
// server is unreachable.
package comfyui_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/comfyui"
)

const (
	e2eEndpoint = "http://127.0.0.1:8188"
	// e2eModel is reported in ImageResponse.Model. It is informational only —
	// the workflow JSON pins the actual UNet (flux-2-klein-4b-fp8.safetensors).
	e2eModel = "flux2-klein-4b-fp8"
)

// TestE2E_SCPCharacter_AngleVariants generates a base SCP-themed character
// with t2i, then re-conditions it from five camera angles via Edit. All seven
// PNGs land in `_bmad-output/comfyui-e2e/<timestamp>/` so the operator can
// open them in any image viewer.
func TestE2E_SCPCharacter_AngleVariants(t *testing.T) {
	if !comfyUIReachable(t) {
		t.Skipf("ComfyUI not reachable at %s — start the server with FLUX.2 Klein 4B FP8 and re-run", e2eEndpoint)
	}

	outDir := newOutputDir(t)
	t.Logf("===> Output directory: %s", outDir)

	httpClient := &http.Client{Timeout: 15 * time.Minute}
	client, err := comfyui.NewImageClient(httpClient, comfyui.ImageClientConfig{
		Endpoint: e2eEndpoint,
		Clock:    clock.RealClock{},
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()

	// Step 1: t2i — generate the base character. ~70s on RX 9060 XT 16GB warm.
	basePath := filepath.Join(outDir, "00_base.png")
	basePrompt := "A mysterious humanoid SCP anomaly entity standing in the center of " +
		"a stark concrete containment chamber, harsh overhead fluorescent lighting " +
		"casting deep shadows on the floor, full-body view from medium distance, " +
		"cinematic horror atmosphere, ominous mood, photorealistic, ultra-detailed, 8k"

	t.Logf("[1/6] t2i base — expected ~70s")
	startBase := time.Now()
	baseResp, err := client.Generate(ctx, domain.ImageRequest{
		Prompt:     basePrompt,
		Model:      e2eModel,
		Width:      2688,
		Height:     1536,
		OutputPath: basePath,
	})
	if err != nil {
		t.Fatalf("Generate(base): %v", err)
	}
	t.Logf("    -> %s (%.1fs wall, %.1fs reported, %d KiB)",
		basePath, time.Since(startBase).Seconds(),
		float64(baseResp.DurationMs)/1000, fileSize(t, basePath)/1024)
	if baseResp.Provider != "comfyui" {
		t.Errorf("provider = %q, want %q", baseResp.Provider, "comfyui")
	}
	if baseResp.CostUSD != 0 {
		t.Errorf("cost = %v, want 0", baseResp.CostUSD)
	}

	// Step 2: load the base PNG as a data URL — Edit's contract per Phase B.
	refDataURL, err := readAsDataURL(basePath, "image/png")
	if err != nil {
		t.Fatalf("readAsDataURL: %v", err)
	}

	// Step 3: edit — five camera/composition variants conditioned on the base.
	angles := []struct {
		slug   string
		prompt string
	}{
		{
			"low_angle_hero",
			"Same character, dramatic low-angle hero shot looking up from the floor, " +
				"the figure looms imposingly, same containment chamber, same harsh fluorescent lighting, " +
				"cinematic horror atmosphere, photorealistic, 8k",
		},
		{
			"over_shoulder",
			"Same character, over-the-shoulder cinematic angle from behind a security guard's vantage, " +
				"the back of a guard's helmet visible in the foreground, the entity in the middle distance, " +
				"same containment chamber, same atmosphere, photorealistic, 8k",
		},
		{
			"profile_side",
			"Same character, strict profile side view, full-body silhouette, " +
				"same chamber lit identically with overhead fluorescents, " +
				"cinematic horror atmosphere, photorealistic, 8k",
		},
		{
			"close_up_face",
			"Same character, close-up portrait of the face and upper torso, " +
				"intense detail on facial features, same harsh fluorescent lighting, " +
				"cinematic horror atmosphere, photorealistic, 8k",
		},
		{
			"wide_establishing",
			"Same character, wide establishing shot showing the full containment chamber, " +
				"the figure as a small distant silhouette in the center, scale emphasized, " +
				"same harsh fluorescent lighting, cinematic horror atmosphere, photorealistic, 8k",
		},
	}

	for i, a := range angles {
		idx := i + 1
		out := filepath.Join(outDir, fmt.Sprintf("%02d_%s.png", idx, a.slug))
		t.Logf("[%d/%d] edit %q — expected ~60s", idx+1, len(angles)+1, a.slug)
		startAngle := time.Now()
		resp, err := client.Edit(ctx, domain.ImageEditRequest{
			Prompt:            a.prompt,
			Model:             e2eModel,
			ReferenceImageURL: refDataURL,
			Width:             2688,
			Height:            1536,
			OutputPath:        out,
		})
		if err != nil {
			t.Errorf("Edit(%s): %v", a.slug, err)
			continue
		}
		t.Logf("    -> %s (%.1fs wall, %.1fs reported, %d KiB)",
			out, time.Since(startAngle).Seconds(),
			float64(resp.DurationMs)/1000, fileSize(t, out)/1024)
	}

	t.Logf("\n===========================================================")
	t.Logf("DONE — open the directory to view results:")
	t.Logf("    %s", outDir)
	t.Logf("===========================================================")
}

// TestE2E_SCP096_AngleVariants — "The Shy Guy". Signature pose: kneeling,
// hands tightly covering face. Standard SCP humanoid containment chamber.
func TestE2E_SCP096_AngleVariants(t *testing.T) {
	if !comfyUIReachable(t) {
		t.Skipf("ComfyUI not reachable at %s", e2eEndpoint)
	}
	runAnglesScenario(t, scenario{
		basePrompt: "SCP-096, a tall extremely slender pale emaciated humanoid covered in patchy " +
			"fine fur, kneeling on the floor of a stark empty concrete containment chamber, " +
			"long thin arms and hands tightly clasped over its hidden face, harsh overhead " +
			"fluorescent lighting, full-body view from medium distance, cinematic SCP Foundation " +
			"horror atmosphere, ominous and unsettling, photorealistic, ultra-detailed, 8k",
		angles: []angleVariant{
			{
				slug: "low_angle_kneeling",
				prompt: "Same SCP-096 creature still kneeling with hands covering face, dramatic " +
					"low-angle shot from the floor looking up, same containment chamber, same " +
					"harsh fluorescent lighting, photorealistic, 8k",
			},
			{
				slug: "wide_establishing",
				prompt: "Same SCP-096 creature kneeling small in the center, wide establishing shot " +
					"of the entire concrete containment chamber, the figure remains the focal " +
					"subject not erased, same harsh fluorescent lighting overhead, photorealistic, 8k",
			},
			{
				slug: "close_up_hands_face",
				prompt: "Same SCP-096 creature, extreme close-up of its long pale fingers tightly " +
					"clasped over its hidden face, fine patchy fur on skin, intense detail, " +
					"same harsh fluorescent lighting, photorealistic, 8k",
			},
			{
				slug: "behind_view_back",
				prompt: "Same SCP-096 creature still kneeling with hands on face, viewed from behind " +
					"showing the slender hairy back and shoulder blades, same containment chamber, " +
					"same lighting, photorealistic, 8k",
			},
			{
				slug: "profile_side",
				prompt: "Same SCP-096 creature kneeling, strict side profile view showing the full " +
					"silhouette and the hands covering the face, same containment chamber, " +
					"same harsh fluorescent lighting, photorealistic, 8k",
			},
		},
	})
}

// TestE2E_SCP096_LoRA_Anime — anime-styled re-take of SCP-096 using the Koni
// anime LoRA. Same signature pose (kneeling, hands covering face) but
// rendered in anime style. Verifies that LoRA injection survives the edit
// workflow on a multi-angle scenario, not only the focused single-angle
// smoke test.
func TestE2E_SCP096_LoRA_Anime(t *testing.T) {
	if !comfyUIReachable(t) {
		t.Skipf("ComfyUI not reachable at %s", e2eEndpoint)
	}
	runAnglesScenario(t, scenario{
		LoRAName:          "Flux_klein_4b_anime_Koni.safetensors",
		LoRAStrengthModel: 1.0,
		LoRAStrengthClip:  1.0,
		basePrompt: "anime style, SCP-096, a tall extremely slender pale emaciated humanoid covered " +
			"in patchy fine fur, kneeling on the floor of a stark empty concrete containment " +
			"chamber, long thin arms and hands tightly clasped over its hidden face, harsh " +
			"overhead fluorescent lighting, full-body view from medium distance, dark anime " +
			"horror atmosphere, ominous and unsettling, detailed line art, vibrant shading, " +
			"high quality, masterpiece",
		angles: []angleVariant{
			{
				slug: "low_angle_kneeling",
				prompt: "anime style, same SCP-096 creature still kneeling with hands covering face, " +
					"dramatic low-angle shot from the floor looking up, same containment chamber, " +
					"same harsh fluorescent lighting, dark anime horror atmosphere, detailed line " +
					"art, high quality, masterpiece",
			},
			{
				slug: "close_up_hands_face",
				prompt: "anime style, same SCP-096 creature, extreme close-up of its long pale " +
					"fingers tightly clasped over its hidden face, fine patchy fur on skin, intense " +
					"detail, same harsh fluorescent lighting, dark anime horror atmosphere, " +
					"detailed line art, high quality, masterpiece",
			},
			{
				slug: "profile_side",
				prompt: "anime style, same SCP-096 creature kneeling, strict side profile view " +
					"showing the full silhouette and the hands covering the face, same containment " +
					"chamber, same harsh fluorescent lighting, dark anime horror atmosphere, " +
					"detailed line art, high quality, masterpiece",
			},
		},
	})
}

// TestE2E_SCP682_AngleVariants — "Hard-to-Destroy Reptile". Massive mutated
// reptilian creature in an acid containment tank. Hostile atmosphere, red
// emergency lighting.
func TestE2E_SCP682_AngleVariants(t *testing.T) {
	if !comfyUIReachable(t) {
		t.Skipf("ComfyUI not reachable at %s", e2eEndpoint)
	}
	runAnglesScenario(t, scenario{
		basePrompt: "SCP-682, a massive grotesque mutated reptilian creature with mottled scaly " +
			"hide and asymmetric deformed limbs, partially submerged in a large industrial acid " +
			"tank in a vast heavily reinforced concrete containment cell, harsh red emergency " +
			"lighting and rising chemical steam, full-body view from medium distance, monstrous " +
			"and hostile expression, cinematic SCP Foundation horror atmosphere, photorealistic, " +
			"ultra-detailed, 8k",
		angles: []angleVariant{
			{
				slug: "low_angle_towering",
				prompt: "Same SCP-682 creature in the same acid tank, dramatic low-angle shot from " +
					"the catwalk looking up at the towering monstrous bulk, same red emergency " +
					"lighting, same chemical steam, photorealistic, 8k",
			},
			{
				slug: "wide_chamber",
				prompt: "Same SCP-682 creature in its acid tank, wide establishing shot of the " +
					"entire reinforced containment cell with catwalks and chemical pipes around " +
					"the tank, the creature remains the focal subject prominently visible, same " +
					"red emergency lighting, same steam, photorealistic, 8k",
			},
			{
				slug: "close_up_head",
				prompt: "Same SCP-682 creature, extreme close-up of its mutated reptilian head " +
					"with cold predatory eyes and rows of teeth, mottled scaly hide visible in " +
					"detail, same red emergency lighting, photorealistic, 8k",
			},
			{
				slug: "side_profile_full_body",
				prompt: "Same SCP-682 creature, strict side profile view of the full body showing " +
					"the asymmetric deformed limbs and massive scale, partially submerged in the " +
					"same acid tank, same red emergency lighting, same steam, photorealistic, 8k",
			},
			{
				slug: "emerging_from_acid",
				prompt: "Same SCP-682 creature lunging upward and rising from the acid in its " +
					"containment tank, splashing acid droplets, aggressive posture, same " +
					"reinforced concrete containment cell, same red emergency lighting and " +
					"steam, photorealistic, 8k",
			},
		},
	})
}

// TestE2E_LoRA_Anime verifies that the runtime LoraLoader injection actually
// reaches ComfyUI and produces an anime-styled image. Generates a t2i base
// with the anime LoRA loaded, then runs one Edit angle so both workflow
// variants exercise the injection path. Uses Flux_klein_4b_anime_Koni
// — the file must already exist under ComfyUI's models/loras/.
func TestE2E_LoRA_Anime(t *testing.T) {
	if !comfyUIReachable(t) {
		t.Skipf("ComfyUI not reachable at %s", e2eEndpoint)
	}

	outDir := newOutputDir(t)
	t.Logf("===> Output directory: %s", outDir)

	httpClient := &http.Client{Timeout: 15 * time.Minute}
	client, err := comfyui.NewImageClient(httpClient, comfyui.ImageClientConfig{
		Endpoint:          e2eEndpoint,
		Clock:             clock.RealClock{},
		LoRAName:          "Flux_klein_4b_anime_Koni.safetensors",
		LoRAStrengthModel: 1.0,
		LoRAStrengthClip:  1.0,
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	basePath := filepath.Join(outDir, "00_base_anime.png")
	basePrompt := "anime style, a young woman with long black hair standing in a sunlit park, " +
		"cherry blossom petals drifting in the air, soft lighting, vibrant colors, " +
		"detailed background, full-body view from medium distance, high quality, masterpiece"

	t.Logf("[1/2] t2i base with LoRA — expected ~70-90s (first run loads LoRA)")
	startBase := time.Now()
	baseResp, err := client.Generate(ctx, domain.ImageRequest{
		Prompt:     basePrompt,
		Model:      e2eModel,
		Width:      2688,
		Height:     1536,
		OutputPath: basePath,
	})
	if err != nil {
		t.Fatalf("Generate(base) with LoRA: %v", err)
	}
	t.Logf("    -> %s (%.1fs wall, %.1fs reported, %d KiB)",
		basePath, time.Since(startBase).Seconds(),
		float64(baseResp.DurationMs)/1000, fileSize(t, basePath)/1024)

	refDataURL, err := readAsDataURL(basePath, "image/png")
	if err != nil {
		t.Fatalf("readAsDataURL: %v", err)
	}

	editPath := filepath.Join(outDir, "01_anime_close_up.png")
	t.Logf("[2/2] edit close-up with LoRA — expected ~60s")
	startEdit := time.Now()
	editResp, err := client.Edit(ctx, domain.ImageEditRequest{
		Prompt: "anime style, same young woman with long black hair, close-up portrait shot, " +
			"soft warm lighting on her face, gentle smile, cherry blossoms blurred in background, " +
			"high quality, masterpiece",
		Model:             e2eModel,
		ReferenceImageURL: refDataURL,
		Width:             2688,
		Height:            1536,
		OutputPath:        editPath,
	})
	if err != nil {
		t.Fatalf("Edit with LoRA: %v", err)
	}
	t.Logf("    -> %s (%.1fs wall, %.1fs reported, %d KiB)",
		editPath, time.Since(startEdit).Seconds(),
		float64(editResp.DurationMs)/1000, fileSize(t, editPath)/1024)

	t.Logf("\n===========================================================")
	t.Logf("DONE — verify the anime style applied:")
	t.Logf("    %s", outDir)
	t.Logf("===========================================================")
}

type scenario struct {
	basePrompt string
	angles     []angleVariant

	// Optional LoRA — when LoRAName is non-empty, the t2i and edit clients
	// inject the LoraLoader at the configured strengths. Empty leaves the
	// base model untouched.
	LoRAName          string
	LoRAStrengthModel float64
	LoRAStrengthClip  float64
}

type angleVariant struct {
	slug   string
	prompt string
}

// runAnglesScenario is the shared driver: t2i base → load as data URL →
// edit each angle. Mirrors TestE2E_SCPCharacter_AngleVariants but factored
// out so SCP-096/682 specs reuse the same orchestration.
func runAnglesScenario(t *testing.T, sc scenario) {
	t.Helper()
	outDir := newOutputDir(t)
	t.Logf("===> Output directory: %s", outDir)

	httpClient := &http.Client{Timeout: 15 * time.Minute}
	client, err := comfyui.NewImageClient(httpClient, comfyui.ImageClientConfig{
		Endpoint:          e2eEndpoint,
		Clock:             clock.RealClock{},
		LoRAName:          sc.LoRAName,
		LoRAStrengthModel: sc.LoRAStrengthModel,
		LoRAStrengthClip:  sc.LoRAStrengthClip,
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()

	if sc.LoRAName != "" {
		t.Logf("LoRA: %s (model=%.2f, clip=%.2f)", sc.LoRAName, sc.LoRAStrengthModel, sc.LoRAStrengthClip)
	}
	basePath := filepath.Join(outDir, "00_base.png")
	t.Logf("[1/%d] t2i base — expected ~70s", len(sc.angles)+1)
	startBase := time.Now()
	baseResp, err := client.Generate(ctx, domain.ImageRequest{
		Prompt:     sc.basePrompt,
		Model:      e2eModel,
		Width:      2688,
		Height:     1536,
		OutputPath: basePath,
	})
	if err != nil {
		t.Fatalf("Generate(base): %v", err)
	}
	t.Logf("    -> %s (%.1fs wall, %.1fs reported, %d KiB)",
		basePath, time.Since(startBase).Seconds(),
		float64(baseResp.DurationMs)/1000, fileSize(t, basePath)/1024)

	refDataURL, err := readAsDataURL(basePath, "image/png")
	if err != nil {
		t.Fatalf("readAsDataURL: %v", err)
	}

	for i, a := range sc.angles {
		out := filepath.Join(outDir, fmt.Sprintf("%02d_%s.png", i+1, a.slug))
		t.Logf("[%d/%d] edit %q — expected ~60s", i+2, len(sc.angles)+1, a.slug)
		startAngle := time.Now()
		resp, err := client.Edit(ctx, domain.ImageEditRequest{
			Prompt:            a.prompt,
			Model:             e2eModel,
			ReferenceImageURL: refDataURL,
			Width:             2688,
			Height:            1536,
			OutputPath:        out,
		})
		if err != nil {
			t.Errorf("Edit(%s): %v", a.slug, err)
			continue
		}
		t.Logf("    -> %s (%.1fs wall, %.1fs reported, %d KiB)",
			out, time.Since(startAngle).Seconds(),
			float64(resp.DurationMs)/1000, fileSize(t, out)/1024)
	}

	t.Logf("\n===========================================================")
	t.Logf("DONE — open the directory to view results:")
	t.Logf("    %s", outDir)
	t.Logf("===========================================================")
}

func comfyUIReachable(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e2eEndpoint+"/system_stats", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func newOutputDir(t *testing.T) string {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	stamp := time.Now().Format("20060102-150405")
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	dir := filepath.Join(root, "_bmad-output", "comfyui-e2e", stamp+"_"+safeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := cwd; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
	}
	return "", fmt.Errorf("go.mod not found upward from %s", cwd)
}

func readAsDataURL(path, mime string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Errorf("stat %s: %v", path, err)
		return 0
	}
	return fi.Size()
}
