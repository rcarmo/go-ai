package durable

func validateCodemodeResult(result CodemodeResult, l Limits) error {
	if !result.HasValue && result.Value != nil {
		return reject("codemode value without presence")
	}
	if result.Error != "" && result.HasValue {
		return reject("codemode error with return value")
	}
	if err := validateCodemodeImages(result.Images, l); err != nil {
		return err
	}
	return validateCodemodeStorePatch(result, l)
}
