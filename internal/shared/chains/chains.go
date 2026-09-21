package chains

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	MonadMainnet = 143
	MonadTestnet = 10143
)

const (
	LabelMonadMainnet = "monad"
	LabelMonadTestnet = "monad-testnet"
)

var byLabel = map[string]int{
	LabelMonadMainnet: MonadMainnet,
	LabelMonadTestnet: MonadTestnet,
}

var byID = map[int]string{
	MonadMainnet: LabelMonadMainnet,
	MonadTestnet: LabelMonadTestnet,
}

func Supported() []int { return []int{MonadMainnet, MonadTestnet} }

func IsSupported(id int) bool { _, ok := byID[id]; return ok }

// 0 when unknown
func ID(label string) int { return byLabel[strings.ToLower(strings.TrimSpace(label))] }

func Label(id int) string { return byID[id] }

// accepts a label or a numeric chain id
func Normalize(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if id, ok := byLabel[s]; ok {
		return id, nil
	}
	if id, err := strconv.Atoi(s); err == nil && IsSupported(id) {
		return id, nil
	}
	return 0, fmt.Errorf("chain %q is not supported; use one of %q, %q or their chain ids", s, LabelMonadMainnet, LabelMonadTestnet)
}
