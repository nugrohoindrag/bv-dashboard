// Package profile: Profile Engine — Property Profile (Hotel/Apartment/Office) menentukan capability, terminologi,
// dan konfigurasi per property tanpa mengubah canonical entity (PRD P1 v1.3 §3, NC v2.0 §7, Onboarding Brief §8/§21).
// Housekeeping, Security, Engineering, Tenant Relation selalu ada pada semua profile (PRD §3.2, guardrail #14).
package profile

import "sort"

type Profile string

const (
	Hotel     Profile = "hotel"
	Apartment Profile = "apartment"
	Office    Profile = "office"
)

var All = []Profile{Hotel, Apartment, Office}

func Valid(p string) bool {
	switch Profile(p) {
	case Hotel, Apartment, Office:
		return true
	}
	return false
}

// Capability = modul/kemampuan yang aktif pada property (server-side; UI hanya mencerminkan).
type Capability string

const (
	// mandatory pada semua profile
	CapHousekeeping   Capability = "housekeeping"
	CapSecurity       Capability = "security"
	CapEngineering    Capability = "engineering"
	CapTenantRelation Capability = "tenant_relation"
	// shared (semua profile)
	CapTenantApp         Capability = "tenant_app"
	CapFacilityBooking   Capability = "facility_booking"
	CapVisitorManagement Capability = "visitor_management"
	CapBilling           Capability = "billing"
	CapVendorManagement  Capability = "vendor_management"
	CapInventory         Capability = "inventory"
	CapReports           Capability = "reports"
	// Hotel
	CapHotelBooking Capability = "hotel_booking"
	CapReception    Capability = "reception"
	// Apartment
	CapUnitSales          Capability = "unit_sales"
	CapUnitRental         Capability = "unit_rental"
	CapResidentManagement Capability = "resident_management"
	// Office
	CapTenantManagement  Capability = "tenant_management"
	CapWorkplaceServices Capability = "workplace_services"
)

// Mandatory tidak dapat dihapus oleh konfigurasi profile mana pun.
var Mandatory = []Capability{CapHousekeeping, CapSecurity, CapEngineering, CapTenantRelation}

var shared = []Capability{CapTenantApp, CapFacilityBooking, CapVisitorManagement, CapBilling, CapVendorManagement, CapInventory, CapReports}

var specific = map[Profile][]Capability{
	Hotel:     {CapHotelBooking, CapReception},
	Apartment: {CapUnitSales, CapUnitRental, CapResidentManagement},
	Office:    {CapTenantManagement, CapWorkplaceServices},
}

// Capabilities: daftar capability aktif untuk profile (terurut, deterministik).
func Capabilities(p Profile) []Capability {
	out := append([]Capability{}, Mandatory...)
	out = append(out, shared...)
	out = append(out, specific[p]...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func Has(p Profile, c Capability) bool {
	for _, x := range Capabilities(p) {
		if x == c {
			return true
		}
	}
	return false
}

// Term: label per bahasa (presentation layer; NC §7 "Profile-Specific User Terminology").
type Term struct {
	ID string `json:"id"`
	EN string `json:"en"`
}

// Terminology keys (stabil; dipakai web, mobile tenant, mobile staff).
const (
	TermCustomer       = "customer"        // end customer
	TermCustomerPlural = "customer_plural" //
	TermOccupant       = "occupant"        // occupying person
	TermRelationModule = "relation_module" // Tenant Relation / Guest Relation / Resident Relation
	TermRequest        = "request"         // customer request
	TermStay           = "stay"            // stay/occupancy context
	TermInventoryUnit  = "inventory_unit"  // commercial inventory: Room / Unit
	TermInventoryUnits = "inventory_units"
	TermMyUnit         = "my_unit" // label tombol "My Unit" di Tenant App
	TermCleaning       = "cleaning_task"
	TermSalesModule    = "commercial_module"
)

var defaults = map[Profile]map[string]Term{
	Hotel: {
		TermCustomer:       {ID: "Tamu", EN: "Guest"},
		TermCustomerPlural: {ID: "Tamu", EN: "Guests"},
		TermOccupant:       {ID: "Tamu", EN: "Guest"},
		TermRelationModule: {ID: "Guest Relation", EN: "Guest Relation"},
		TermRequest:        {ID: "Guest Service Request", EN: "Guest Service Request"},
		TermStay:           {ID: "Menginap", EN: "Stay"},
		TermInventoryUnit:  {ID: "Kamar", EN: "Room"},
		TermInventoryUnits: {ID: "Kamar", EN: "Rooms"},
		TermMyUnit:         {ID: "Kamar Saya", EN: "My Room"},
		TermCleaning:       {ID: "Room Cleaning", EN: "Room Cleaning"},
		TermSalesModule:    {ID: "Hotel Booking Management", EN: "Hotel Booking Management"},
	},
	Apartment: {
		TermCustomer:       {ID: "Penghuni", EN: "Resident"},
		TermCustomerPlural: {ID: "Penghuni", EN: "Residents"},
		TermOccupant:       {ID: "Penghuni", EN: "Occupant"},
		TermRelationModule: {ID: "Resident Relation", EN: "Resident Relation"},
		TermRequest:        {ID: "Resident Service Request", EN: "Resident Service Request"},
		TermStay:           {ID: "Hunian", EN: "Residency"},
		TermInventoryUnit:  {ID: "Unit", EN: "Unit"},
		TermInventoryUnits: {ID: "Unit", EN: "Units"},
		TermMyUnit:         {ID: "Unit Saya", EN: "My Unit"},
		TermCleaning:       {ID: "Unit Cleaning", EN: "Unit Cleaning"},
		TermSalesModule:    {ID: "Unit Sales & Rental", EN: "Unit Sales & Rental"},
	},
	Office: {
		TermCustomer:       {ID: "Tenant", EN: "Tenant"},
		TermCustomerPlural: {ID: "Tenant", EN: "Tenants"},
		TermOccupant:       {ID: "Penghuni", EN: "Occupant"},
		TermRelationModule: {ID: "Tenant Relation", EN: "Tenant Relation"},
		TermRequest:        {ID: "Tenant Service Request", EN: "Tenant Service Request"},
		TermStay:           {ID: "Tenancy", EN: "Tenancy"},
		TermInventoryUnit:  {ID: "Unit", EN: "Unit"},
		TermInventoryUnits: {ID: "Unit", EN: "Units"},
		TermMyUnit:         {ID: "Unit Saya", EN: "My Unit"},
		TermCleaning:       {ID: "Office Cleaning", EN: "Office Cleaning"},
		TermSalesModule:    {ID: "Commercial", EN: "Commercial"},
	},
}

// DefaultTerminology mengembalikan salinan terminologi default profile.
func DefaultTerminology(p Profile) map[string]Term {
	out := map[string]Term{}
	for k, v := range defaults[p] {
		out[k] = v
	}
	return out
}

// Describe: label & deskripsi profile untuk UI pemilihan saat Create Property (Onboarding Brief §6).
type Info struct {
	Code         Profile      `json:"code"`
	Label        string       `json:"label"`
	Description  Term         `json:"description"`
	Capabilities []Capability `json:"capabilities"`
}

func Catalog() []Info {
	return []Info{
		{Code: Hotel, Label: "Hotel", Description: Term{ID: "Untuk hotel, kamar tamu, dan operasi hospitality", EN: "For hotels, guest rooms and hospitality operations"}, Capabilities: Capabilities(Hotel)},
		{Code: Apartment, Label: "Apartment", Description: Term{ID: "Untuk gedung hunian dan layanan penghuni", EN: "For residential buildings and resident services"}, Capabilities: Capabilities(Apartment)},
		{Code: Office, Label: "Office", Description: Term{ID: "Untuk gedung perkantoran dan layanan tenant", EN: "For office buildings and tenant services"}, Capabilities: Capabilities(Office)},
	}
}

// DefaultCategories: kategori Service Request default per profile (PRD §11 minimum + domain routing).
type CategorySeed struct {
	Code, Name, NameID, Domain, Priority, Icon string
	Profiles                                   []Profile
}

func DefaultCategories() []CategorySeed {
	all := []Profile{Hotel, Apartment, Office}
	return []CategorySeed{
		{"plumbing", "Plumbing", "Perpipaan / Saluran Air", "engineering", "high", "plumbing", all},
		{"electrical", "Electrical", "Elektrikal", "engineering", "high", "electrical_services", all},
		{"air_conditioning", "Air Conditioning", "Tata Udara (AC)", "engineering", "medium", "ac_unit", all},
		{"cleanliness", "Cleanliness", "Kebersihan", "housekeeping", "medium", "cleaning_services", all},
		{"security", "Security", "Keamanan", "security", "high", "security", all},
		{"lift", "Lift", "Lift", "engineering", "high", "elevator", all},
		{"facility", "Facility", "Fasilitas", "engineering", "medium", "meeting_room", all},
		{"noise", "Noise", "Kebisingan", "management", "low", "volume_up", all},
		{"pest", "Pest", "Pengendalian Hama", "housekeeping", "medium", "pest_control", all},
		{"building_damage", "Building Damage", "Kerusakan Bangunan", "engineering", "medium", "construction", all},
		{"room_service", "Room Service", "Layanan Kamar", "housekeeping", "medium", "room_service", []Profile{Hotel}},
		{"guest_amenities", "Guest Amenities", "Amenities Tamu", "housekeeping", "low", "spa", []Profile{Hotel}},
		{"renovation", "Renovation", "Renovasi", "management", "low", "handyman", []Profile{Apartment}},
		{"gardening", "Gardening", "Pertamanan", "housekeeping", "low", "yard", []Profile{Apartment, Office}},
		{"access_card", "Access Card", "Kartu Akses", "security", "medium", "badge", []Profile{Office, Apartment}},
		{"other", "Other", "Lainnya", "management", "low", "more_horiz", all},
	}
}

func ProfilesToStrings(ps []Profile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, string(p))
	}
	return out
}
