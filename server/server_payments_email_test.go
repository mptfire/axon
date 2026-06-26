//go:build !nopayments

package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v74"
	"heckel.io/ntfy/v2/user"
)

// stripeCheckoutMock wires up a testStripeAPI for a successful checkout of user u, with the given
// billing email on the session's CustomerDetails.
func stripeCheckoutMock(u *user.User, billingEmail string) *testStripeAPI {
	m := &testStripeAPI{}
	m.On("GetSession", "SOMETOKEN").Return(&stripe.CheckoutSession{
		ClientReferenceID: u.ID,
		Customer:          &stripe.Customer{ID: "acct_5555"},
		Subscription:      &stripe.Subscription{ID: "sub_1234"},
		CustomerDetails:   &stripe.CheckoutSessionCustomerDetails{Email: billingEmail},
	}, nil)
	m.On("GetSubscription", "sub_1234").Return(&stripe.Subscription{
		ID:               "sub_1234",
		Status:           stripe.SubscriptionStatusActive,
		CurrentPeriodEnd: 123456789,
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{Price: &stripe.Price{ID: "price_1234", Recurring: &stripe.PriceRecurring{Interval: stripe.PriceRecurringIntervalMonth}}},
			},
		},
	}, nil)
	m.On("UpdateCustomer", "acct_5555", mock.Anything).Return(&stripe.Customer{}, nil)
	return m
}

func newCheckoutEmailTestServer(t *testing.T, databaseURL string) (*Server, *captureMailer, *user.User) {
	c := newTestConfigWithAuthFile(t, databaseURL)
	c.StripeSecretKey = "secret key"
	c.BaseURL = "https://ntfy.example.com"
	c.SMTPSenderAddr = "localhost:25"
	c.SMTPSenderFrom = "noreply@example.com"
	s := newTestServer(t, c)
	mailer := newCaptureMailer()
	s.mailer = mailer
	require.Nil(t, s.userManager.AddTier(&user.Tier{
		ID: "ti_123", Code: "starter", StripeMonthlyPriceID: "price_1234", MessageLimit: 100, MessageExpiryDuration: time.Hour,
	}))
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleUser, false))
	u, err := s.userManager.User("phil")
	require.Nil(t, err)
	return s, mailer, u
}

func TestPayments_Checkout_SendsBillingEmailVerification(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, u := newCheckoutEmailTestServer(t, databaseURL)
		defer s.closeDatabases()
		s.stripe = stripeCheckoutMock(u, "billing@example.com")

		rr := request(t, s, "GET", "/v1/account/billing/subscription/success/SOMETOKEN", "", nil)
		require.Equal(t, 303, rr.Code)

		// A verification link was auto-sent to the billing email; clicking it verifies + sets primary
		link := mailer.verifyLinks["billing@example.com"]
		require.NotEmpty(t, link)
		token := tokenFromLink(t, link, "https://ntfy.example.com/account/email/verify/")
		require.Equal(t, 200, request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, token), nil).Code)

		emails, err := s.userManager.Emails(u.ID)
		require.Nil(t, err)
		require.Equal(t, []string{"billing@example.com"}, emails.Strings())
		primary, err := s.userManager.PrimaryEmail(u.ID)
		require.Nil(t, err)
		require.Equal(t, "billing@example.com", primary)
	})
}

func TestPayments_Checkout_SkipsBillingEmailWhenAlreadyVerified(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, u := newCheckoutEmailTestServer(t, databaseURL)
		defer s.closeDatabases()
		s.stripe = stripeCheckoutMock(u, "billing@example.com")

		// User already has a verified email -> no auto-send on checkout
		require.Nil(t, s.userManager.AddEmail(u.ID, "existing@example.com"))

		rr := request(t, s, "GET", "/v1/account/billing/subscription/success/SOMETOKEN", "", nil)
		require.Equal(t, 303, rr.Code)
		require.Empty(t, mailer.verifyLinks)
	})
}

func TestPayments_Checkout_SkipsBillingEmailWhenPrimaryElsewhere(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, u := newCheckoutEmailTestServer(t, databaseURL)
		defer s.closeDatabases()
		s.stripe = stripeCheckoutMock(u, "billing@example.com")

		// The billing email is already the recovery email on another account -> skip
		require.Nil(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
		alice, err := s.userManager.User("alice")
		require.Nil(t, err)
		require.Nil(t, s.userManager.AddEmail(alice.ID, "billing@example.com"))
		require.Nil(t, s.userManager.SetPrimaryEmail(alice.ID, "billing@example.com"))

		rr := request(t, s, "GET", "/v1/account/billing/subscription/success/SOMETOKEN", "", nil)
		require.Equal(t, 303, rr.Code)
		require.Empty(t, mailer.verifyLinks)
	})
}
