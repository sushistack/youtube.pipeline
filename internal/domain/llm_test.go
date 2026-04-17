package domain

import "context"

// Compile-time interface satisfaction checks.
var _ TextGenerator = (*mockTextGen)(nil)
var _ ImageGenerator = (*mockImageGen)(nil)
var _ TTSSynthesizer = (*mockTTSSynth)(nil)

type mockTextGen struct{}

func (m *mockTextGen) Generate(_ context.Context, _ TextRequest) (TextResponse, error) {
	return TextResponse{}, nil
}

type mockImageGen struct{}

func (m *mockImageGen) Generate(_ context.Context, _ ImageRequest) (ImageResponse, error) {
	return ImageResponse{}, nil
}

func (m *mockImageGen) Edit(_ context.Context, _ ImageEditRequest) (ImageResponse, error) {
	return ImageResponse{}, nil
}

type mockTTSSynth struct{}

func (m *mockTTSSynth) Synthesize(_ context.Context, _ TTSRequest) (TTSResponse, error) {
	return TTSResponse{}, nil
}
