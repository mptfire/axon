//go:build nopayments

package payments

const Available = false

type SubscriptionStatus string

type PriceRecurringInterval string

func Setup(stripeSecretKey string) {
	// Nothing to see here
}
