package trust

import (
	"crypto/x509"
	"time"
)

// Result is the qualification of a timestamp token (spec §6 step 6).
type Result int

const (
	Unconfirmed  Result = iota // "unbestätigt": no verified list is fresh enough to decide
	Qualified                  // "qualifiziert"
	NotQualified               // "nicht qualifiziert": the deciding list shows no grant at genTime
)

// String returns the spec term used in reports.
func (r Result) String() string {
	switch r {
	case Qualified:
		return "qualifiziert"
	case NotQualified:
		return "nicht qualifiziert"
	default:
		return "unbestätigt"
	}
}

// Decision is a qualification result and the list it rests on. For
// Unconfirmed, the list fields name the newest list that lists the signer but
// is not fresh enough, or whose entry for genTime states no identity; they
// are empty if no list lists the signer. TSPName and Service are empty when
// the provider's list lists no matching service.
type Decision struct {
	Result        Result
	Territory     string
	Sequence      int
	IssueDate     time.Time
	FetchedOnline bool   // the list was downloaded and verified by this run
	TSPName       string // the matching service, if any
	Service       string
}

// qualify applies spec §6 step 6 to the newest verified list of each
// territory. path is the signer certificate followed by its validated chain.
//
// A list may decide only if it was issued after genTime (the issue date is
// signed), or if this process fetched and verified it online after genTime.
// The deciding list then yields Qualified if a matching service had, in the
// history entry for genTime, the type QTST, the status granted and an
// identity in path; else NotQualified. With no deciding list: Unconfirmed.
// A deciding list whose otherwise qualifying entry for genTime states no
// identity yields Unconfirmed, unless another list yields Qualified.
//
// territory is the trusted list of the provider that issued the token ("" if
// unknown). If no list decides otherwise and that list can decide, it shows no
// QTST service for the path at genTime: NotQualified. The hint can only turn
// Unconfirmed into NotQualified, never produce Qualified.
func qualify(lists []*List, path []*x509.Certificate, genTime time.Time, territory string) Decision {
	best := Decision{Result: Unconfirmed}
	var indeterminate *Decision // a deciding list that cannot tell
	for _, l := range lists {
		services := l.matching(path)
		if len(services) == 0 {
			continue
		}
		d := listDecision(l, Unconfirmed)
		if decides(l, genTime) {
			d.Result = NotQualified
			d.TSPName, d.Service = services[0].TSPName, services[0].Name
			var unknown *Service
			for i, s := range services {
				r := s.qualificationAt(path, genTime)
				if r == Qualified {
					d.Result = Qualified
					d.TSPName, d.Service = s.TSPName, s.Name
					break
				}
				if r == Unconfirmed && unknown == nil {
					unknown = &services[i]
				}
			}
			if d.Result != Qualified && unknown != nil && indeterminate == nil {
				u := listDecision(l, Unconfirmed)
				u.TSPName, u.Service = unknown.TSPName, unknown.Name
				indeterminate = &u
			}
		}
		if rank(d) > rank(best) {
			best = d
		}
	}
	if best.Result != Qualified && indeterminate != nil {
		return *indeterminate
	}
	if best.Result != Unconfirmed || territory == "" {
		return best
	}
	for _, l := range lists {
		if l.Territory == territory && decides(l, genTime) {
			return listDecision(l, NotQualified)
		}
	}
	return best
}

// decides reports whether l may decide about genTime (spec §6 step 6).
func decides(l *List, genTime time.Time) bool {
	return l.IssueDate.After(genTime) || l.fetchedOnline.After(genTime)
}

func listDecision(l *List, r Result) Decision {
	return Decision{
		Result:        r,
		Territory:     l.Territory,
		Sequence:      l.Sequence,
		IssueDate:     l.IssueDate,
		FetchedOnline: !l.fetchedOnline.IsZero(),
	}
}

// rank orders decisions when several territories list the signer.
func rank(d Decision) int {
	switch {
	case d.Result == Qualified:
		return 3
	case d.Result == NotQualified:
		return 2
	case d.Territory != "":
		return 1
	default:
		return 0
	}
}

// matching returns the services that, in some history entry, name the
// signer or a CA in its path as their ServiceDigitalIdentity.
func (l *List) matching(path []*x509.Certificate) []Service {
	var out []Service
	for _, s := range l.Services {
		if s.lists(path) {
			out = append(out, s)
		}
	}
	return out
}
