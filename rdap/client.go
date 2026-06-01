package rdap

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// Client performs RDAP lookups, routing each request through the
// IANA bootstrap registry to the correct Regional Internet Registry.
type Client struct {
	http *http.Client
	boot *Bootstrap
}

// NewClient returns a Client using boot for RDAP server resolution.
func NewClient(boot *Bootstrap) *Client {
	return &Client{
		http: &http.Client{},
		boot: boot,
	}
}

// LookupIP queries the RDAP registry for a single IP address.
func (c *Client) LookupIP(ctx context.Context, ip *pb.IP) (*pb.RDAPResponse, error) {
	netIP := ipFromProto(ip)
	if netIP == nil {
		return nil, fmt.Errorf("invalid IP message")
	}
	baseURL, err := c.boot.Resolve(netIP)
	if err != nil {
		return nil, err
	}
	return c.query(ctx, baseURL+"ip/"+renderNetIP(netIP), baseURL)
}

// LookupCIDR queries the RDAP registry for a CIDR block. The full
// prefix is forwarded (e.g. "10.0.0.0/8") so the RIR returns the
// registration for that exact block.
func (c *Client) LookupCIDR(ctx context.Context, cidr *pb.CIDR) (*pb.RDAPResponse, error) {
	netIP := ipFromProto(cidr.GetIp())
	if netIP == nil {
		return nil, fmt.Errorf("invalid CIDR IP")
	}
	prefix, err := subnetPrefixLen(cidr.GetSubnet())
	if err != nil {
		return nil, err
	}
	baseURL, err := c.boot.Resolve(netIP)
	if err != nil {
		return nil, err
	}
	return c.query(ctx, fmt.Sprintf("%sip/%s/%d", baseURL, renderNetIP(netIP), prefix), baseURL)
}

// LookupAutnum queries the RDAP registry for an Autonomous System Number.
func (c *Client) LookupAutnum(ctx context.Context, asn *pb.ASN) (*pb.RDAPAutnumResponse, error) {
	baseURL, err := c.boot.ResolveASN(asn.GetNumber())
	if err != nil {
		return nil, err
	}
	return c.queryAutnum(ctx, fmt.Sprintf("%sautnum/%d", baseURL, asn.GetNumber()), baseURL)
}

func (c *Client) queryAutnum(ctx context.Context, queryURL, rdapServer string) (*pb.RDAPAutnumResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RDAP GET %s: %w", queryURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading RDAP response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RDAP server returned HTTP %d for %s: %s",
			resp.StatusCode, queryURL, truncate(string(body), 200))
	}

	autnum, err := parseAutnum(body, rdapServer)
	if err != nil {
		return nil, fmt.Errorf("parsing RDAP autnum response: %w", err)
	}
	return &pb.RDAPAutnumResponse{Autnum: autnum, RawJson: string(body)}, nil
}

func (c *Client) query(ctx context.Context, queryURL, rdapServer string) (*pb.RDAPResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RDAP GET %s: %w", queryURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading RDAP response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RDAP server returned HTTP %d for %s: %s",
			resp.StatusCode, queryURL, truncate(string(body), 200))
	}

	network, err := parseNetwork(body, rdapServer)
	if err != nil {
		return nil, fmt.Errorf("parsing RDAP response: %w", err)
	}
	return &pb.RDAPResponse{Network: network, RawJson: string(body)}, nil
}

// --- enum conversion maps (JSON string → proto enum) ---

var ipVersionMap = map[string]pb.RDAPIPVersion{
	"v4": pb.RDAPIPVersion_RDAP_IP_VERSION_V4,
	"v6": pb.RDAPIPVersion_RDAP_IP_VERSION_V6,
}

var roleMap = map[string]pb.RDAPRole{
	"registrant":             pb.RDAPRole_RDAP_ROLE_REGISTRANT,
	"technical":              pb.RDAPRole_RDAP_ROLE_TECHNICAL,
	"administrative":         pb.RDAPRole_RDAP_ROLE_ADMINISTRATIVE,
	"abuse":                  pb.RDAPRole_RDAP_ROLE_ABUSE,
	"noc":                    pb.RDAPRole_RDAP_ROLE_NOC,
	"billing":                pb.RDAPRole_RDAP_ROLE_BILLING,
	"registrar":              pb.RDAPRole_RDAP_ROLE_REGISTRAR,
	"reseller":               pb.RDAPRole_RDAP_ROLE_RESELLER,
	"sponsor":                pb.RDAPRole_RDAP_ROLE_SPONSOR,
	"proxy":                  pb.RDAPRole_RDAP_ROLE_PROXY,
	"notifications":          pb.RDAPRole_RDAP_ROLE_NOTIFICATIONS,
	"nocDefaultAbuseContact": pb.RDAPRole_RDAP_ROLE_NOC_DEFAULT_ABUSE_CONTACT,
	"routing":                pb.RDAPRole_RDAP_ROLE_ROUTING,
}

var eventActionMap = map[string]pb.RDAPEventAction{
	"registration":                 pb.RDAPEventAction_RDAP_EVENT_ACTION_REGISTRATION,
	"reregistration":               pb.RDAPEventAction_RDAP_EVENT_ACTION_REREGISTRATION,
	"last changed":                 pb.RDAPEventAction_RDAP_EVENT_ACTION_LAST_CHANGED,
	"expiration":                   pb.RDAPEventAction_RDAP_EVENT_ACTION_EXPIRATION,
	"deletion":                     pb.RDAPEventAction_RDAP_EVENT_ACTION_DELETION,
	"reinstantiation":              pb.RDAPEventAction_RDAP_EVENT_ACTION_REINSTANTIATION,
	"transfer":                     pb.RDAPEventAction_RDAP_EVENT_ACTION_TRANSFER,
	"locked":                       pb.RDAPEventAction_RDAP_EVENT_ACTION_LOCKED,
	"unlocked":                     pb.RDAPEventAction_RDAP_EVENT_ACTION_UNLOCKED,
	"last update of RDAP database": pb.RDAPEventAction_RDAP_EVENT_ACTION_LAST_UPDATE_OF_RDAP_DATABASE,
	"registrar expiration":         pb.RDAPEventAction_RDAP_EVENT_ACTION_REGISTRAR_EXPIRATION,
	"enum validation expiration":   pb.RDAPEventAction_RDAP_EVENT_ACTION_ENUM_VALIDATION_EXPIRATION,
}

var statusMap = map[string]pb.RDAPStatus{
	"active":              pb.RDAPStatus_RDAP_STATUS_ACTIVE,
	"inactive":            pb.RDAPStatus_RDAP_STATUS_INACTIVE,
	"validated":           pb.RDAPStatus_RDAP_STATUS_VALIDATED,
	"renew prohibited":    pb.RDAPStatus_RDAP_STATUS_RENEW_PROHIBITED,
	"update prohibited":   pb.RDAPStatus_RDAP_STATUS_UPDATE_PROHIBITED,
	"transfer prohibited": pb.RDAPStatus_RDAP_STATUS_TRANSFER_PROHIBITED,
	"delete prohibited":   pb.RDAPStatus_RDAP_STATUS_DELETE_PROHIBITED,
	"proxy":               pb.RDAPStatus_RDAP_STATUS_PROXY,
	"private":             pb.RDAPStatus_RDAP_STATUS_PRIVATE,
	"removed":             pb.RDAPStatus_RDAP_STATUS_REMOVED,
	"obscured":            pb.RDAPStatus_RDAP_STATUS_OBSCURED,
	"associated":          pb.RDAPStatus_RDAP_STATUS_ASSOCIATED,
	"locked":              pb.RDAPStatus_RDAP_STATUS_LOCKED,
	"pending create":      pb.RDAPStatus_RDAP_STATUS_PENDING_CREATE,
	"pending renew":       pb.RDAPStatus_RDAP_STATUS_PENDING_RENEW,
	"pending transfer":    pb.RDAPStatus_RDAP_STATUS_PENDING_TRANSFER,
	"pending update":      pb.RDAPStatus_RDAP_STATUS_PENDING_UPDATE,
	"pending delete":      pb.RDAPStatus_RDAP_STATUS_PENDING_DELETE,
	"server recover prohibited": pb.RDAPStatus_RDAP_STATUS_RECOVER_PROHIBITED,
	"client recover prohibited": pb.RDAPStatus_RDAP_STATUS_RECOVER_PROHIBITED,
}

var entityKindMap = map[string]pb.RDAPEntityKind{
	"individual":  pb.RDAPEntityKind_RDAP_ENTITY_KIND_INDIVIDUAL,
	"group":       pb.RDAPEntityKind_RDAP_ENTITY_KIND_GROUP,
	"org":         pb.RDAPEntityKind_RDAP_ENTITY_KIND_ORG,
	"location":    pb.RDAPEntityKind_RDAP_ENTITY_KIND_LOCATION,
	"application": pb.RDAPEntityKind_RDAP_ENTITY_KIND_APPLICATION,
}

// --- JSON parsing ---

type rdapJSON struct {
	Handle          string       `json:"handle"`
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	StartAddress    string       `json:"startAddress"`
	EndAddress      string       `json:"endAddress"`
	IPVersion       string       `json:"ipVersion"`
	Country         string       `json:"country"`
	Status          []string     `json:"status"`
	Entities        []entityJSON `json:"entities"`
	Events          []eventJSON  `json:"events"`
	Links           []linkJSON   `json:"links"`
	ParentHandle    string       `json:"parentHandle"`
	CIDR0CIDRs      []cidr0JSON  `json:"cidr0_cidrs"`
	RDAPConformance []string     `json:"rdapConformance"`
}

type entityJSON struct {
	Handle     string          `json:"handle"`
	VcardArray json.RawMessage `json:"vcardArray"`
	Roles      []string        `json:"roles"`
}

type eventJSON struct {
	EventAction string `json:"eventAction"`
	EventDate   string `json:"eventDate"`
}

type linkJSON struct {
	Href string `json:"href"`
}

type cidr0JSON struct {
	V4Prefix string `json:"v4prefix"`
	V6Prefix string `json:"v6prefix"`
	Length   uint32 `json:"length"`
}

func parseNetwork(body []byte, rdapServer string) (*pb.RDAPNetwork, error) {
	var r rdapJSON
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}

	entities := make([]*pb.RDAPEntity, 0, len(r.Entities))
	for _, e := range r.Entities {
		vc := parseVCard(e.VcardArray)
		roles := make([]pb.RDAPRole, 0, len(e.Roles))
		for _, rs := range e.Roles {
			roles = append(roles, roleMap[rs])
		}
		entities = append(entities, &pb.RDAPEntity{
			Handle:  e.Handle,
			Fn:      vc.FN,
			Roles:   roles,
			Emails:  vc.Emails,
			Kind:    entityKindMap[vc.Kind],
			Org:     vc.Org,
			Address: vc.Address,
			Phone:   vc.Phone,
		})
	}

	events := make([]*pb.RDAPEvent, 0, len(r.Events))
	for _, ev := range r.Events {
		events = append(events, &pb.RDAPEvent{
			Action: eventActionMap[ev.EventAction],
			Date:   ev.EventDate,
		})
	}

	statuses := make([]pb.RDAPStatus, 0, len(r.Status))
	for _, s := range r.Status {
		statuses = append(statuses, statusMap[s])
	}

	links := make([]string, 0, len(r.Links))
	for _, l := range r.Links {
		if l.Href != "" {
			links = append(links, l.Href)
		}
	}

	cidrBlocks := make([]*pb.RDAPCIDRBlock, 0, len(r.CIDR0CIDRs))
	for _, c := range r.CIDR0CIDRs {
		prefix := c.V4Prefix
		if prefix == "" {
			prefix = c.V6Prefix
		}
		if prefix != "" {
			cidrBlocks = append(cidrBlocks, &pb.RDAPCIDRBlock{
				Prefix: prefix,
				Length: c.Length,
			})
		}
	}

	return &pb.RDAPNetwork{
		Handle:          r.Handle,
		Name:            r.Name,
		Type:            r.Type,
		StartAddress:    r.StartAddress,
		EndAddress:      r.EndAddress,
		IpVersion:       ipVersionMap[r.IPVersion],
		Country:         r.Country,
		Status:          statuses,
		Entities:        entities,
		Events:          events,
		Links:           links,
		RdapServer:      rdapServer,
		ParentHandle:    r.ParentHandle,
		CidrBlocks:      cidrBlocks,
		RdapConformance: r.RDAPConformance,
	}, nil
}

type autnumJSON struct {
	Handle          string       `json:"handle"`
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Country         string       `json:"country"`
	StartAutnum     uint32       `json:"startAutnum"`
	EndAutnum       uint32       `json:"endAutnum"`
	Status          []string     `json:"status"`
	Entities        []entityJSON `json:"entities"`
	Events          []eventJSON  `json:"events"`
	Links           []linkJSON   `json:"links"`
	RDAPConformance []string     `json:"rdapConformance"`
}

func parseAutnum(body []byte, rdapServer string) (*pb.RDAPAutnum, error) {
	var r autnumJSON
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}

	entities := make([]*pb.RDAPEntity, 0, len(r.Entities))
	for _, e := range r.Entities {
		vc := parseVCard(e.VcardArray)
		roles := make([]pb.RDAPRole, 0, len(e.Roles))
		for _, rs := range e.Roles {
			roles = append(roles, roleMap[rs])
		}
		entities = append(entities, &pb.RDAPEntity{
			Handle:  e.Handle,
			Fn:      vc.FN,
			Roles:   roles,
			Emails:  vc.Emails,
			Kind:    entityKindMap[vc.Kind],
			Org:     vc.Org,
			Address: vc.Address,
			Phone:   vc.Phone,
		})
	}

	events := make([]*pb.RDAPEvent, 0, len(r.Events))
	for _, ev := range r.Events {
		events = append(events, &pb.RDAPEvent{
			Action: eventActionMap[ev.EventAction],
			Date:   ev.EventDate,
		})
	}

	statuses := make([]pb.RDAPStatus, 0, len(r.Status))
	for _, s := range r.Status {
		statuses = append(statuses, statusMap[s])
	}

	links := make([]string, 0, len(r.Links))
	for _, l := range r.Links {
		if l.Href != "" {
			links = append(links, l.Href)
		}
	}

	return &pb.RDAPAutnum{
		Handle:          r.Handle,
		Name:            r.Name,
		Type:            r.Type,
		Country:         r.Country,
		StartAutnum:     r.StartAutnum,
		EndAutnum:       r.EndAutnum,
		Status:          statuses,
		Entities:        entities,
		Events:          events,
		Links:           links,
		RdapServer:      rdapServer,
		RdapConformance: r.RDAPConformance,
	}, nil
}

// vcardResult holds the fields extracted from a vCard.
type vcardResult struct {
	FN      string
	Kind    string
	Org     string
	Address string
	Phone   string
	Emails  []string
}

// parseVCard extracts structured fields from an RDAP vCard.
// The vcardArray format per RFC 6350 / RFC 7483 is:
//
//	["vcard", [["version",{},"text","4.0"], ["fn",{},"text","Name"], ...]]
//
// Each entry is [propName, params, type, value]. The params object (index 1)
// may carry a "label" key for adr entries.
func parseVCard(raw json.RawMessage) vcardResult {
	var res vcardResult
	if len(raw) == 0 {
		return res
	}
	// Top level: ["vcard", [...entries...]]
	var top []json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || len(top) < 2 {
		return res
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(top[1], &entries); err != nil {
		return res
	}
	for _, entry := range entries {
		var fields []json.RawMessage
		if err := json.Unmarshal(entry, &fields); err != nil || len(fields) < 4 {
			continue
		}
		var prop string
		if err := json.Unmarshal(fields[0], &prop); err != nil {
			continue
		}
		switch prop {
		case "fn":
			json.Unmarshal(fields[3], &res.FN) //nolint:errcheck
		case "kind":
			json.Unmarshal(fields[3], &res.Kind) //nolint:errcheck
		case "org":
			json.Unmarshal(fields[3], &res.Org) //nolint:errcheck
		case "email":
			var email string
			if err := json.Unmarshal(fields[3], &email); err == nil && email != "" {
				res.Emails = append(res.Emails, email)
			}
		case "tel":
			if res.Phone == "" {
				json.Unmarshal(fields[3], &res.Phone) //nolint:errcheck
			}
		case "adr":
			// Prefer the label parameter (formatted address string) over the
			// structured 7-element array, which ARIN leaves entirely empty.
			var params struct {
				Label string `json:"label"`
			}
			if err := json.Unmarshal(fields[1], &params); err == nil && params.Label != "" {
				res.Address = params.Label
			}
		}
	}
	return res
}

// --- helpers ---

func ipFromProto(ip *pb.IP) net.IP {
	if ip == nil {
		return nil
	}
	buf := make(net.IP, 16)
	binary.BigEndian.PutUint64(buf[0:8], uint64(ip.GetNetworkPrefix()))
	binary.BigEndian.PutUint64(buf[8:16], uint64(ip.GetInterfaceIdentifier()))
	return buf
}

func renderNetIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func subnetPrefixLen(s *pb.Subnet) (uint32, error) {
	if s == nil {
		return 0, fmt.Errorf("nil subnet")
	}
	switch f := s.GetFormat().(type) {
	case *pb.Subnet_PrefixLength:
		return f.PrefixLength, nil
	case *pb.Subnet_Text:
		txt := f.Text
		if len(txt) > 0 && txt[0] == '/' {
			txt = txt[1:]
		}
		var n uint32
		if _, err := fmt.Sscanf(txt, "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid prefix text %q: %w", f.Text, err)
		}
		return n, nil
	case *pb.Subnet_Netmask:
		ip := net.ParseIP(f.Netmask).To4()
		if ip == nil {
			return 0, fmt.Errorf("invalid netmask %q", f.Netmask)
		}
		ones, _ := net.IPMask(ip).Size()
		return uint32(ones), nil
	}
	return 0, fmt.Errorf("subnet has no format set")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
