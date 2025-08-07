//go:build !nopayments

package payments

import "github.com/stripe/stripe-go/v74"

const Available = true

type SubscriptionStatus stripe.SubscriptionStatus

type PriceRecurringInterval stripe.PriceRecurringInterval

func Setup(stripeSecretKey string) {
	stripe.EnableTelemetry = false // Whoa!
	stripe.Key = stripeSecretKey
}
