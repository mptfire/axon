//go:build nopayments

package payments

// Available is a constant used to indicate that Stripe support is available.
// It can be disabled with the 'nopayments' build tag.
const Available = false

// SubscriptionStatus is a dummy type
type SubscriptionStatus string

// PriceRecurringInterval is dummy type
type PriceRecurringInterval string

// Setup is a dummy type
func Setup(stripeSecretKey string) {
	// Nothing to see here
}
