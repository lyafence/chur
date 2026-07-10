//go:build !exclude_test_deps

package webhook

import (
	_ "github.com/lyafence/chur/internal/providers/env"
	_ "github.com/lyafence/chur/internal/providers/keeper"
	_ "github.com/lyafence/chur/internal/providers/local"
)
