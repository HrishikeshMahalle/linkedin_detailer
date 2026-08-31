package profile

import "time"

const SchemaVersion = "1.0"

type Result struct {
	SchemaVersion string  `json:"schema_version"`
	Profile       Profile `json:"profile"`
	Meta          Meta    `json:"meta"`
}

type Meta struct {
	RequestID string    `json:"request_id,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	CacheHit  bool      `json:"cache_hit"`
	Partial   bool      `json:"partial"`
	Warnings  []string  `json:"warnings"`
}

type Profile struct {
	PublicIdentifier string          `json:"public_identifier"`
	ProfileURL       string          `json:"profile_url"`
	FirstName        string          `json:"first_name,omitempty"`
	LastName         string          `json:"last_name,omitempty"`
	FullName         string          `json:"full_name,omitempty"`
	Headline         string          `json:"headline,omitempty"`
	Location         string          `json:"location,omitempty"`
	About            string          `json:"about,omitempty"`
	Images           []Image         `json:"profile_images"`
	Experience       []Experience    `json:"experience"`
	Education        []Education     `json:"education"`
	Skills           []Skill         `json:"skills"`
	Certifications   []Certification `json:"certifications"`
	Languages        []Language      `json:"languages"`
}

type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type PartialDate struct {
	Year  int `json:"year,omitempty"`
	Month int `json:"month,omitempty"`
}

type DateRange struct {
	Start     *PartialDate `json:"start,omitempty"`
	End       *PartialDate `json:"end,omitempty"`
	IsCurrent bool         `json:"is_current"`
}

type Experience struct {
	CompanyID   string     `json:"company_id,omitempty"`
	CompanyName string     `json:"company_name,omitempty"`
	CompanyURL  string     `json:"company_url,omitempty"`
	Logo        []Image    `json:"logo,omitempty"`
	Positions   []Position `json:"positions"`
}

type Position struct {
	Title          string    `json:"title,omitempty"`
	EmploymentType string    `json:"employment_type,omitempty"`
	Location       string    `json:"location,omitempty"`
	Description    string    `json:"description,omitempty"`
	DateRange      DateRange `json:"date_range"`
}

type Education struct {
	SchoolID     string    `json:"school_id,omitempty"`
	SchoolName   string    `json:"school_name,omitempty"`
	SchoolURL    string    `json:"school_url,omitempty"`
	DegreeName   string    `json:"degree_name,omitempty"`
	FieldOfStudy string    `json:"field_of_study,omitempty"`
	Grade        string    `json:"grade,omitempty"`
	Description  string    `json:"description,omitempty"`
	DateRange    DateRange `json:"date_range"`
	Logo         []Image   `json:"logo,omitempty"`
}

type Skill struct {
	Name         string `json:"name"`
	Endorsements int    `json:"endorsements,omitempty"`
}

type Certification struct {
	Name                string       `json:"name,omitempty"`
	IssuingOrganization string       `json:"issuing_organization,omitempty"`
	IssueDate           *PartialDate `json:"issue_date,omitempty"`
	ExpirationDate      *PartialDate `json:"expiration_date,omitempty"`
	CredentialID        string       `json:"credential_id,omitempty"`
	CredentialURL       string       `json:"credential_url,omitempty"`
}

type Language struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency,omitempty"`
}
