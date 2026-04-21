// Package openalex provides types and a thin HTTP client for the OpenAlex API.
//
// Types are derived from the OpenAPI 3.1 spec at
// /Users/esh/Documents/webapps/apis/openalex/openapi.yaml
// and mirror the curated surface used by the TypeScript sibling at
// /Users/esh/Documents/webapps/papers/src/openalex/types.ts — only the Works
// and Authors entities, which is the subset needed to enrich Zotero items
// (zot find / zot add --openalex / zot doctor --enrich).
//
// Authentication is optional and additive: Email populates the "polite pool"
// (~10 req/s) and APIKey unlocks the premium tier (~100 req/s). Both flow as
// query parameters, not headers.
package openalex

// Work is a scholarly document (article, preprint, book chapter, etc.).
// Response from GET /works/{id}.
type Work struct {
	ID                           string              `json:"id"`
	DOI                          *string             `json:"doi"`
	Title                        *string             `json:"title"`
	DisplayName                  *string             `json:"display_name"`
	PublicationYear              *int                `json:"publication_year"`
	PublicationDate              *string             `json:"publication_date"`
	Type                         *string             `json:"type"`
	Language                     *string             `json:"language"`
	IsRetracted                  bool                `json:"is_retracted"`
	IsOA                         bool                `json:"is_oa"`
	CitedByCount                 int                 `json:"cited_by_count"`
	ReferencedWorksCount         int                 `json:"referenced_works_count"`
	FWCI                         *float64            `json:"fwci"`
	CitationNormalizedPercentile *CitationPercentile `json:"citation_normalized_percentile"`
	HasFulltext                  bool                `json:"has_fulltext"`
	PrimaryLocation              *Location           `json:"primary_location"`
	BestOALocation               *Location           `json:"best_oa_location"`
	OpenAccess                   *OpenAccess         `json:"open_access"`
	Authorships                  []Authorship        `json:"authorships"`
	Topics                       []Topic             `json:"topics"`
	Keywords                     []Keyword           `json:"keywords"`
	ReferencedWorks              []string            `json:"referenced_works"`
	Mesh                         []Mesh              `json:"mesh"`
	AbstractInvertedIndex        map[string][]int    `json:"abstract_inverted_index"`
}

type CitationPercentile struct {
	Value float64 `json:"value"`
}

type Location struct {
	Source         *SourceRef `json:"source"`
	LandingPageURL *string    `json:"landing_page_url"`
	PDFURL         *string    `json:"pdf_url"`
	IsOA           bool       `json:"is_oa"`
	Version        *string    `json:"version"`
}

type SourceRef struct {
	ID                   string  `json:"id"`
	DisplayName          string  `json:"display_name"`
	ISSNL                *string `json:"issn_l"`
	Type                 *string `json:"type"`
	HostOrganization     *string `json:"host_organization"`
	HostOrganizationName *string `json:"host_organization_name"`
}

type OpenAccess struct {
	IsOA     bool    `json:"is_oa"`
	OAStatus string  `json:"oa_status"`
	OAURL    *string `json:"oa_url"`
}

type Authorship struct {
	AuthorPosition        string        `json:"author_position"`
	Author                AuthorRef     `json:"author"`
	Institutions          []Institution `json:"institutions"`
	IsCorresponding       bool          `json:"is_corresponding"`
	RawAffiliationStrings []string      `json:"raw_affiliation_strings"`
}

type AuthorRef struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	ORCID       *string `json:"orcid"`
}

type Institution struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	ROR         *string `json:"ror"`
	CountryCode *string `json:"country_code"`
	Type        *string `json:"type"`
}

type Topic struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Score       float64   `json:"score"`
	Subfield    *TopicRef `json:"subfield"`
	Field       *TopicRef `json:"field"`
	Domain      *TopicRef `json:"domain"`
}

type TopicRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Keyword struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Score       float64 `json:"score"`
}

type Mesh struct {
	DescriptorUI   string  `json:"descriptor_ui"`
	DescriptorName string  `json:"descriptor_name"`
	QualifierUI    string  `json:"qualifier_ui"`
	QualifierName  *string `json:"qualifier_name"`
	IsMajorTopic   bool    `json:"is_major_topic"`
}

// Author is the full Author entity from GET /authors/{id}.
type Author struct {
	ID                    string        `json:"id"`
	DisplayName           string        `json:"display_name"`
	ORCID                 *string       `json:"orcid"`
	WorksCount            int           `json:"works_count"`
	CitedByCount          int           `json:"cited_by_count"`
	SummaryStats          *SummaryStats `json:"summary_stats"`
	LastKnownInstitutions []Institution `json:"last_known_institutions"`
}

type SummaryStats struct {
	HIndex   int `json:"h_index"`
	I10Index int `json:"i10_index"`
}

// Source is the full Source entity (journal, repository, conference proceedings)
// from GET /sources/{id}.
type Source struct {
	ID                   string        `json:"id"`
	DisplayName          string        `json:"display_name"`
	Type                 *string       `json:"type"`
	ISSNL                *string       `json:"issn_l"`
	ISSN                 []string      `json:"issn"`
	Publisher            *string       `json:"publisher"`
	CountryCode          *string       `json:"country_code"`
	IsOA                 bool          `json:"is_oa"`
	IsInDOAJ             bool          `json:"is_in_doaj"`
	HostOrganization     *string       `json:"host_organization"`
	HostOrganizationName *string       `json:"host_organization_name"`
	WorksCount           int           `json:"works_count"`
	CitedByCount         int           `json:"cited_by_count"`
	SummaryStats         *SummaryStats `json:"summary_stats"`
}

// Results is a paginated response wrapper used by /works, /authors, /sources, etc.
type Results[T any] struct {
	Meta    ResultsMeta `json:"meta"`
	Results []T         `json:"results"`
}

type ResultsMeta struct {
	Count      int     `json:"count"`
	PerPage    int     `json:"per_page"`
	Page       *int    `json:"page,omitempty"`
	NextCursor *string `json:"next_cursor,omitempty"`
}
