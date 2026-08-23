//go:build !darwin

package product

func runServicePlatform(action string, opts ServiceOptions) operationResult {
	return errorResult("unsupported_platform", "product service management is supported only on macOS", nil)
}
