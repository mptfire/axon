//go:build !nopayments

package payments

import "github.com/stripe/stripe-go/v74"

// Available is a constant used to indicate that Stripe support is available.
// It can be disabled with the 'nopayments' build tag.
const Available = true

// SubscriptionStatus is an alias for stripe.SubscriptionStatus
type SubscriptionStatus stripe.SubscriptionStatus

// PriceRecurringInterval is an alias for stripe.PriceRecurringInterval
type PriceRecurringInterval stripe.PriceRecurringInterval

// Setup sets the Stripe secret key and disables telemetry
func Setup(stripeSecretKey string) {
	stripe.EnableTelemetry = false // Whoa!
	stripe.Key = stripeSecretKey
}
