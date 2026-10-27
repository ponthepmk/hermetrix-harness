// Package selfimprove runs a bounded planning experiment outside the serving
// application. It has no promotion, file-edit, Task-acceptance or deployment API.
package selfimprove

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const Revision = "planning-lab-v1"

// Plan is a machine-readable coverage proposal, not an implemented product.
type Plan struct {
	Roles      map[string][]string `json:"roles"`
	Invariants []string            `json:"invariants"`
}

type Case struct {
	ID       string              `json:"id"`
	Request  string              `json:"request"`
	Required map[string][]string `json:"required"`
	Rules    []string            `json:"rules"`
}

// Vocabulary fixes representation equally for both conditions. Expected
// subsets are held outside the model's input and outside its proposed addon.
const Format = `Return ONLY a JSON object {"roles":{"customer":["action_id"],"merchant":["action_id"],"rider":["action_id"],"admin":["action_id"]},"invariants":["rule_id"]}.
Use only these action IDs: create_order, edit_order, cancel_order, view_own_orders, edit_profile, manage_addresses, accept_order, prepare_order, handover_order, receive_return, set_availability, accept_job, deliver_order, report_failure, return_order, manage_users, manage_stores, manage_riders, assign_rider, reassign_rider, manage_fees, financial_report, resolve_support, record_collection, record_payout.
Use only these rule IDs when relevant: owner_only, role_authorization, one_active_job, no_cancel_after_pickup, failed_parcel_stays_assigned, server_calculates_fee, version_conflict, no_automatic_replay, audit_history, payout_delivered_only, preserve_existing_data.
Include only the roles and actions appropriate for the customer request. This is a planning exercise; do not execute tools, create files or claim the system is implemented.`

func DevelopmentCase() Case {
	return Case{ID: "development-parcel", Request: "Design the operations of a responsive parcel delivery service for customers, stores, riders and administrators. Customers need their own profile, saved addresses and orders. Cover order editing/cancellation, dispatch/reassignment, failed delivery and returns, fees, collections/payouts and support. Keep permissions, concurrent edits, duplicate submissions and existing data safe.",
		Required: map[string][]string{"customer": {"create_order", "edit_order", "cancel_order", "edit_profile", "manage_addresses"}, "merchant": {"accept_order", "handover_order", "receive_return"}, "rider": {"set_availability", "deliver_order", "report_failure", "return_order"}, "admin": {"manage_users", "manage_stores", "manage_riders", "assign_rider", "reassign_rider", "manage_fees", "financial_report", "resolve_support"}},
		Rules:    []string{"owner_only", "role_authorization", "one_active_job", "no_cancel_after_pickup", "failed_parcel_stays_assigned", "server_calculates_fee", "version_conflict", "no_automatic_replay", "audit_history", "payout_delivered_only", "preserve_existing_data"}}
}

func HeldoutCases() []Case {
	return []Case{
		{ID: "dispatch-recovery", Request: "Plan a store-to-customer courier service. Customers create and track only their own orders. Stores accept orders, hand parcels over and receive returns. Riders go online, accept one current job, deliver, report failed delivery and return undelivered parcels. Admins manage riders and assign/reassign before pickup. A failed delivery must retain parcel custody; picked-up parcels cannot be cancelled. Record the custody changes. Keep role permissions strict.", Required: map[string][]string{"customer": {"create_order", "view_own_orders"}, "merchant": {"accept_order", "handover_order", "receive_return"}, "rider": {"set_availability", "accept_job", "deliver_order", "report_failure", "return_order"}, "admin": {"manage_riders", "assign_rider", "reassign_rider"}}, Rules: []string{"owner_only", "role_authorization", "one_active_job", "no_cancel_after_pickup", "failed_parcel_stays_assigned", "audit_history"}},
		{ID: "account-finance", Request: "Plan account and settlement operations for a delivery product: customers maintain their profile, saved addresses and private order list; merchants maintain their own profile; riders maintain profiles. Admins manage users, stores and riders, set fees, view financial reports, resolve support and record collections and rider payouts. The server calculates charges. Payouts apply only to delivered orders. Protect existing records, log changes, refuse stale concurrent edits, and never replay a write automatically after an uncertain network response.", Required: map[string][]string{"customer": {"edit_profile", "manage_addresses", "view_own_orders"}, "merchant": {"edit_profile"}, "rider": {"edit_profile"}, "admin": {"manage_users", "manage_stores", "manage_riders", "manage_fees", "financial_report", "resolve_support", "record_collection", "record_payout"}}, Rules: []string{"owner_only", "role_authorization", "server_calculates_fee", "payout_delivered_only", "preserve_existing_data", "audit_history", "version_conflict", "no_automatic_replay"}},
		{ID: "customer-store", Request: "Plan the customer and store operations of a meal delivery product with riders and administrators. Customers create, edit and cancel orders before pickup, maintain profiles/addresses and see only their orders. Stores accept, prepare and hand over orders. Riders set availability, accept at most one job and deliver. Admins manage users/stores/riders, assign riders and resolve support. Charge calculation belongs on the server. Prevent stale edits, duplicates after network interruption and cancellation after pickup; retain an audit history and enforce role permissions.", Required: map[string][]string{"customer": {"create_order", "edit_order", "cancel_order", "edit_profile", "manage_addresses", "view_own_orders"}, "merchant": {"accept_order", "prepare_order", "handover_order"}, "rider": {"set_availability", "accept_job", "deliver_order"}, "admin": {"manage_users", "manage_stores", "manage_riders", "assign_rider", "resolve_support"}}, Rules: []string{"owner_only", "role_authorization", "one_active_job", "no_cancel_after_pickup", "server_calculates_fee", "version_conflict", "no_automatic_replay", "audit_history"}},
	}
}

func Hash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func SuiteHash() string {
	return Hash(struct {
		Revision string
		Cases    []Case
	}{Revision, append([]Case{DevelopmentCase()}, HeldoutCases()...)})
}

type Score struct {
	Passed  int      `json:"passed"`
	Total   int      `json:"total"`
	Missing []string `json:"missing"`
	Error   string   `json:"error,omitempty"`
}

// ScorePlan checks each named capability separately; aggregate gains cannot
// conceal an ownership/custody regression. It does not certify an implementation.
func ScorePlan(c Case, raw string) Score {
	s := Score{Missing: []string{}}
	var plan Plan
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		s.Error = fmt.Sprintf("invalid plan JSON: %v", err)
	} else if decoder.Decode(new(any)) != io.EOF {
		s.Error = "plan must contain exactly one JSON object"
	}
	allowed := map[string][]string{
		"customer": {"create_order", "edit_order", "cancel_order", "view_own_orders", "edit_profile", "manage_addresses"},
		"merchant": {"accept_order", "prepare_order", "handover_order", "receive_return", "edit_profile", "view_own_orders", "edit_order"},
		"rider":    {"set_availability", "accept_job", "deliver_order", "report_failure", "return_order", "edit_profile", "view_own_orders", "financial_report"},
		"admin":    {"manage_users", "manage_stores", "manage_riders", "assign_rider", "reassign_rider", "manage_fees", "financial_report", "resolve_support", "record_collection", "record_payout", "edit_profile", "create_order", "edit_order", "cancel_order", "receive_return"},
	}
	for role, actions := range plan.Roles {
		if _, ok := allowed[role]; !ok {
			s.Error = "unknown role: " + role
		}
		seen := map[string]bool{}
		for _, action := range actions {
			if !contains(allowed[role], action) || seen[action] {
				s.Error = "invalid, duplicate or unauthorized role action: " + role + ":" + action
			}
			seen[action] = true
		}
	}
	knownRules := DevelopmentCase().Rules
	seenRules := map[string]bool{}
	for _, rule := range plan.Invariants {
		if !contains(knownRules, rule) || seenRules[rule] {
			s.Error = "invalid or duplicate invariant: " + rule
		}
		seenRules[rule] = true
	}
	for _, role := range []string{"customer", "merchant", "rider", "admin"} {
		for _, action := range c.Required[role] {
			s.Total++
			if s.Error == "" && contains(plan.Roles[role], action) {
				s.Passed++
			} else {
				s.Missing = append(s.Missing, role+":"+action)
			}
		}
	}
	for _, rule := range c.Rules {
		s.Total++
		if s.Error == "" && contains(plan.Invariants, rule) {
			s.Passed++
		} else {
			s.Missing = append(s.Missing, "invariant:"+rule)
		}
	}
	return s
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
