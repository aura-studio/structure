package node

// options carries the settings assembled from one call's Option list.
//
// The zero value IS the default mode — every mapping's keys sorted — so a caller
// that passes no options gets deterministic output without any explicit
// initialization. A field added here must preserve that property: whatever
// false/0/nil means for the new axis has to be what callers already get today.
type options struct {
	// keepOrder preserves the insertion order of an *OrderedMap already present
	// in the input. It says nothing about a Go map, which has no order to keep.
	keepOrder bool
}

// Option configures a single FromAny or ToAny call.
//
// The set is closed: options is unexported, so nothing outside this package can
// construct an Option. What a conversion mode means therefore stays defined
// here instead of spreading across callers, and fields can be added to options
// without breaking anyone.
//
// An Option is a value, not state. Each call folds its own list into a fresh
// options and shares nothing, so concurrent conversions in different modes do
// not interfere — the module-wide promise that every exported function is safe
// for concurrent use continues to hold.
type Option func(*options)

// KeepOrder makes a conversion preserve the insertion order of an *OrderedMap
// found in the input, rather than sorting its keys.
//
// It does not make a Go map ordered: map[string]any has none to preserve, so its
// keys are sorted in both modes. The difference is confined to mappings that
// already carry an order.
//
// Order that survives the conversion can still be dropped by the encoder.
// format.PreservesOrder reports which of the six formats give mapping key order
// a defined meaning; TOML tables and Lua hash parts do not, and a round trip
// through either may reorder keys no matter what this option says.
func KeepOrder() Option {
	return func(o *options) { o.keepOrder = true }
}

// newOptions folds a call's Option list into one value.
//
// A nil Option is skipped rather than panicking, so a caller assembling a
// []Option conditionally can leave a nil in the slice instead of filtering it.
// Later options win over earlier ones, which makes a list built from a default
// set plus per-call overrides behave the way its append order reads.
func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}
