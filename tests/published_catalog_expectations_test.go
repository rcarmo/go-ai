package goai_test

// Sizes of the pinned published catalog. Update them together after the
// full-record regeneration check, regardless of when a live lookup test began.
const (
	publishedChatModels    = 1701
	publishedChatProviders = 42
	publishedChatAPIs      = 10

	publishedImageModels    = 74
	publishedImageProviders = 1
	publishedImageAPIs      = 1

	publishedClassifierModels    = 83
	publishedClassifierProviders = 7
	publishedClassifierAPIs      = 3

	publishedModelTotal = publishedChatModels + publishedImageModels + publishedClassifierModels
)
