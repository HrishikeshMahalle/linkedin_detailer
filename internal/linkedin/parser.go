package linkedin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/tradelab/linkedin-profile-api/internal/profile"
)

func ParseProfileView(publicIdentifier string, body []byte) (profile.Profile, []string, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return profile.Profile{}, nil, fmt.Errorf("decode profile response: %w", err)
	}
	resolveNormalizedResponse(root)

	profileMap := findProfile(root, publicIdentifier)
	if profileMap == nil {
		return profile.Profile{}, nil, errors.New("profile object is missing")
	}

	result := profile.Profile{
		PublicIdentifier: publicIdentifier,
		ProfileURL:       "https://www.linkedin.com/in/" + url.PathEscape(publicIdentifier),
		FirstName:        firstString(profileMap, "firstName", "multiLocaleFirstName", "first_name"),
		LastName:         firstString(profileMap, "lastName", "multiLocaleLastName", "last_name"),
		Headline:         firstString(profileMap, "headline", "multiLocaleHeadline", "occupation"),
		Location:         firstString(profileMap, "geoLocationName", "locationName", "multiLocaleAddress", "location"),
		About:            firstString(profileMap, "summary", "multiLocaleSummary", "about"),
		Images:           collectProfileImages(profileMap),
		Experience:       []profile.Experience{},
		Education:        []profile.Education{},
		Skills:           []profile.Skill{},
		Certifications:   []profile.Certification{},
		Languages:        []profile.Language{},
	}
	result.FullName = strings.TrimSpace(strings.Join(nonEmpty(result.FirstName, result.LastName), " "))
	if result.FullName == "" {
		result.FullName = firstString(profileMap, "fullName", "name")
	}
	if result.Location == "" {
		result.Location = nestedString(profileMap, []string{"geoLocation", "geo", "defaultLocalizedName"}, "")
	}

	warnings := make([]string, 0, 5)
	if values, ok := sectionElements(root, profileMap, "positionView", "profilePositionGroups", "PositionGroup", "Position"); ok {
		result.Experience = parseExperience(values)
	} else {
		warnings = append(warnings, "experience section was not returned by LinkedIn")
	}
	if values, ok := sectionElements(root, profileMap, "educationView", "profileEducations", "Education"); ok {
		result.Education = parseEducation(values)
	} else {
		warnings = append(warnings, "education section was not returned by LinkedIn")
	}
	if values, ok := sectionElements(root, profileMap, "skillView", "profileSkills", "Skill"); ok {
		result.Skills = parseSkills(values)
	} else {
		warnings = append(warnings, "skills section was not returned by LinkedIn")
	}
	if values, ok := sectionElements(root, profileMap, "certificationView", "profileCertifications", "Certification"); ok {
		result.Certifications = parseCertifications(values)
	} else {
		warnings = append(warnings, "certifications section was not returned by LinkedIn")
	}
	if values, ok := sectionElements(root, profileMap, "languageView", "profileLanguages", "Language"); ok {
		result.Languages = parseLanguages(values)
	} else {
		warnings = append(warnings, "languages section was not returned by LinkedIn")
	}

	return result, warnings, nil
}

func parseExperience(values []any) []profile.Experience {
	result := make([]profile.Experience, 0, len(values))
	byCompany := make(map[string]int)

	for _, value := range values {
		item := asMap(value)
		if item == nil {
			continue
		}

		company := companyDetails(item)
		positions := mapsFrom(item["positions"])
		if len(positions) == 0 {
			positionCollection := asMap(item["profilePositionInPositionGroup"])
			positions = mapsFrom(positionCollection["elements"])
		}
		if len(positions) == 0 {
			positions = []map[string]any{item}
		}

		key := company.CompanyID
		if key == "" {
			key = strings.ToLower(company.CompanyName)
		}
		if key == "" {
			key = "position-" + strconv.Itoa(len(result))
		}

		index, exists := byCompany[key]
		if !exists {
			index = len(result)
			byCompany[key] = index
			result = append(result, company)
		}
		for _, positionMap := range positions {
			result[index].Positions = append(result[index].Positions, parsePosition(positionMap))
		}
	}
	return result
}

func companyDetails(item map[string]any) profile.Experience {
	company := asMap(item["company"])
	miniCompany := asMap(company["miniCompany"])
	if miniCompany == nil {
		miniCompany = company
	}
	name := firstString(item, "companyName", "multiLocaleCompanyName")
	if name == "" {
		name = firstString(miniCompany, "name")
	}
	urn := firstString(item, "companyUrn")
	if urn == "" {
		urn = firstString(miniCompany, "entityUrn", "objectUrn")
	}
	id := idFromURN(urn)
	result := profile.Experience{
		CompanyID:   id,
		CompanyName: name,
		Logo:        collectImages(miniCompany),
		Positions:   []profile.Position{},
	}
	if id != "" {
		result.CompanyURL = "https://www.linkedin.com/company/" + url.PathEscape(id)
	}
	return result
}

func parsePosition(item map[string]any) profile.Position {
	return profile.Position{
		Title:          firstString(item, "title", "multiLocaleTitle", "name"),
		EmploymentType: nestedString(item, []string{"employmentType", "name"}, "employmentType"),
		Location:       firstString(item, "locationName", "multiLocaleLocationName", "location"),
		Description:    firstString(item, "description", "multiLocaleDescription"),
		DateRange:      firstDateRange(item),
	}
}

func parseEducation(values []any) []profile.Education {
	result := make([]profile.Education, 0, len(values))
	for _, value := range values {
		item := asMap(value)
		if item == nil {
			continue
		}
		school := asMap(item["school"])
		miniSchool := asMap(school["miniSchool"])
		if miniSchool == nil {
			miniSchool = school
		}
		name := firstString(item, "schoolName", "multiLocaleSchoolName")
		if name == "" {
			name = firstString(miniSchool, "name")
		}
		urn := firstString(item, "schoolUrn")
		if urn == "" {
			urn = firstString(miniSchool, "entityUrn", "objectUrn")
		}
		id := idFromURN(urn)
		entry := profile.Education{
			SchoolID:     id,
			SchoolName:   name,
			DegreeName:   firstString(item, "degreeName", "multiLocaleDegreeName", "degree"),
			FieldOfStudy: firstString(item, "fieldOfStudy", "multiLocaleFieldOfStudy", "fieldOfStudyName"),
			Grade:        firstString(item, "grade"),
			Description:  firstString(item, "description", "multiLocaleDescription", "activities"),
			DateRange:    firstDateRange(item),
			Logo:         collectImages(miniSchool),
		}
		if id != "" {
			entry.SchoolURL = "https://www.linkedin.com/school/" + url.PathEscape(id)
		}
		result = append(result, entry)
	}
	return result
}

func parseSkills(values []any) []profile.Skill {
	result := make([]profile.Skill, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		item := asMap(value)
		if item == nil {
			continue
		}
		skillMap := asMap(item["skill"])
		name := firstString(item, "name", "multiLocaleName")
		if name == "" {
			name = firstString(skillMap, "name", "multiLocaleName")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, profile.Skill{
			Name:         name,
			Endorsements: firstInt(item, "endorsementCount", "numEndorsements"),
		})
	}
	return result
}

func parseCertifications(values []any) []profile.Certification {
	result := make([]profile.Certification, 0, len(values))
	for _, value := range values {
		item := asMap(value)
		if item == nil {
			continue
		}
		authority := asMap(item["authority"])
		timePeriod := asMap(item["timePeriod"])
		if timePeriod == nil {
			timePeriod = asMap(item["dateRange"])
		}
		dateRange := parseDateRange(timePeriod)
		result = append(result, profile.Certification{
			Name:                firstString(item, "name", "multiLocaleName"),
			IssuingOrganization: firstNonEmpty(firstString(item, "authorityName", "multiLocaleAuthority"), firstString(authority, "name")),
			IssueDate:           dateRange.Start,
			ExpirationDate:      dateRange.End,
			CredentialID:        firstString(item, "licenseNumber", "credentialId"),
			CredentialURL:       firstString(item, "url", "credentialUrl"),
		})
	}
	return result
}

func parseLanguages(values []any) []profile.Language {
	result := make([]profile.Language, 0, len(values))
	for _, value := range values {
		item := asMap(value)
		if item == nil {
			continue
		}
		name := firstString(item, "name", "multiLocaleName")
		if name == "" {
			continue
		}
		result = append(result, profile.Language{
			Name:        name,
			Proficiency: firstString(item, "proficiency", "proficiencyName"),
		})
	}
	return result
}

func firstDateRange(value map[string]any) profile.DateRange {
	if timePeriod := asMap(value["timePeriod"]); timePeriod != nil {
		return parseDateRange(timePeriod)
	}
	return parseDateRange(asMap(value["dateRange"]))
}

func parseDateRange(value map[string]any) profile.DateRange {
	if value == nil {
		return profile.DateRange{}
	}
	startMap := asMap(value["startDate"])
	if startMap == nil {
		startMap = asMap(value["start"])
	}
	endMap := asMap(value["endDate"])
	if endMap == nil {
		endMap = asMap(value["end"])
	}
	start := parsePartialDate(startMap)
	end := parsePartialDate(endMap)
	return profile.DateRange{
		Start:     start,
		End:       end,
		IsCurrent: start != nil && end == nil,
	}
}

func parsePartialDate(value map[string]any) *profile.PartialDate {
	if value == nil {
		return nil
	}
	date := &profile.PartialDate{
		Year:  firstInt(value, "year"),
		Month: firstInt(value, "month"),
	}
	if date.Year == 0 && date.Month == 0 {
		return nil
	}
	return date
}

func sectionElements(root, profileMap map[string]any, legacyName, dashName string, typeSuffixes ...string) ([]any, bool) {
	if values, ok := viewElements(root, legacyName); ok {
		return values, true
	}
	if value, exists := profileMap[dashName]; exists {
		switch typed := value.(type) {
		case []any:
			return typed, true
		case map[string]any:
			if values, ok := typed["elements"].([]any); ok {
				return values, true
			}
			typeName := firstString(typed, "$type")
			for _, suffix := range typeSuffixes {
				if strings.HasSuffix(typeName, suffix) {
					return []any{typed}, true
				}
			}
		}
	}
	if values := includedByType(root, typeSuffixes...); len(values) > 0 {
		return values, true
	}
	return nil, false
}

func viewElements(root map[string]any, name string) ([]any, bool) {
	view, exists := root[name]
	if !exists {
		if data := asMap(root["data"]); data != nil {
			view, exists = data[name]
		}
	}
	if !exists {
		return nil, false
	}
	viewMap := asMap(view)
	if viewMap == nil {
		return []any{}, true
	}
	values, ok := viewMap["elements"].([]any)
	if !ok {
		return []any{}, true
	}
	return values, true
}

func includedByType(root map[string]any, suffixes ...string) []any {
	for _, suffix := range suffixes {
		result := make([]any, 0)
		for _, item := range mapsFrom(root["included"]) {
			typeName := firstString(item, "$type")
			if strings.HasSuffix(typeName, suffix) {
				result = append(result, item)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return nil
}

func findProfile(root map[string]any, publicIdentifier string) map[string]any {
	if direct := asMap(root["profile"]); direct != nil {
		return direct
	}
	if data := asMap(root["data"]); data != nil {
		if direct := asMap(data["profile"]); direct != nil {
			return direct
		}
		if direct := firstProfileCandidate(data["elements"], publicIdentifier); direct != nil {
			return direct
		}
	}
	if direct := firstProfileCandidate(root["elements"], publicIdentifier); direct != nil {
		return direct
	}
	return firstProfileCandidate(root["included"], publicIdentifier)
}

func firstProfileCandidate(value any, publicIdentifier string) map[string]any {
	for _, item := range mapsFrom(value) {
		identifier := firstString(item, "publicIdentifier")
		urn := firstString(item, "entityUrn")
		hasName := firstString(item, "firstName", "multiLocaleFirstName") != ""
		if strings.EqualFold(identifier, publicIdentifier) || (hasName && strings.Contains(urn, "fsd_profile:")) {
			return item
		}
	}
	return nil
}

func resolveNormalizedResponse(root map[string]any) {
	index := make(map[string]map[string]any)
	for _, item := range mapsFrom(root["included"]) {
		if urn := firstString(item, "entityUrn"); urn != "" {
			index[urn] = item
		}
	}
	if len(index) == 0 {
		return
	}

	var resolve func(any, int, map[string]bool) any
	resolve = func(value any, depth int, active map[string]bool) any {
		if depth > 12 {
			return value
		}
		switch typed := value.(type) {
		case map[string]any:
			urn := firstString(typed, "entityUrn")
			if urn != "" {
				if active[urn] {
					return typed
				}
				active[urn] = true
				defer delete(active, urn)
			}
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			for _, key := range keys {
				child := typed[key]
				typed[key] = resolve(child, depth+1, active)
				if !strings.HasPrefix(key, "*") {
					continue
				}
				resolvedKey := strings.TrimPrefix(key, "*")
				if _, exists := typed[resolvedKey]; exists {
					continue
				}
				if resolved := resolveReferences(child, index, resolve, depth+1, active); resolved != nil {
					typed[resolvedKey] = resolved
				}
			}
			return typed
		case []any:
			for index := range typed {
				typed[index] = resolve(typed[index], depth+1, active)
			}
			return typed
		default:
			return value
		}
	}
	resolve(root, 0, make(map[string]bool))
}

func resolveReferences(
	value any,
	index map[string]map[string]any,
	resolve func(any, int, map[string]bool) any,
	depth int,
	active map[string]bool,
) any {
	switch typed := value.(type) {
	case string:
		if entity := index[typed]; entity != nil {
			return resolve(entity, depth+1, active)
		}
	case map[string]any:
		return resolve(typed, depth+1, active)
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			if urn, ok := item.(string); ok {
				if entity := index[urn]; entity != nil {
					result = append(result, resolve(entity, depth+1, active))
				}
				continue
			}
			result = append(result, resolve(item, depth+1, active))
		}
		return result
	}
	return nil
}

func collectImages(value any) []profile.Image {
	result := make([]profile.Image, 0)
	seen := make(map[string]struct{})
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			rootURL := firstString(typed, "rootUrl")
			if rootURL != "" {
				if artifacts, ok := typed["artifacts"].([]any); ok {
					for _, artifactValue := range artifacts {
						artifact := asMap(artifactValue)
						path := firstString(artifact, "fileIdentifyingUrlPathSegment")
						imageURL := rootURL + path
						if imageURL == "" {
							continue
						}
						if _, exists := seen[imageURL]; exists {
							continue
						}
						seen[imageURL] = struct{}{}
						result = append(result, profile.Image{
							URL:    imageURL,
							Width:  firstInt(artifact, "width"),
							Height: firstInt(artifact, "height"),
						})
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func collectProfileImages(value map[string]any) []profile.Image {
	result := make([]profile.Image, 0)
	seen := make(map[string]struct{})
	add := func(source any) {
		for _, image := range collectImages(source) {
			if _, exists := seen[image.URL]; exists {
				continue
			}
			seen[image.URL] = struct{}{}
			result = append(result, image)
		}
	}
	for _, key := range []string{"profilePicture", "backgroundPicture", "backgroundImage", "picture"} {
		add(value[key])
	}
	if miniProfile := asMap(value["miniProfile"]); miniProfile != nil {
		add(miniProfile["picture"])
		add(miniProfile["backgroundImage"])
	}
	return result
}

func mapsFrom(value any) []map[string]any {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(values))
	for _, item := range values {
		if mapped := asMap(item); mapped != nil {
			result = append(result, mapped)
		}
	}
	return result
}

func asMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func firstString(value map[string]any, keys ...string) string {
	if value == nil {
		return ""
	}
	for _, key := range keys {
		switch typed := value[key].(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case map[string]any:
			if localized := localizedString(typed); localized != "" {
				return localized
			}
		}
	}
	return ""
}

func localizedString(value map[string]any) string {
	preferred := []string{"en_US", "en", "defaultLocalizedName", "localized", "value", "text", "name"}
	for _, key := range preferred {
		switch typed := value[key].(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case map[string]any:
			if nested := localizedString(typed); nested != "" {
				return nested
			}
		}
	}

	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if typed, ok := value[key].(string); ok && strings.TrimSpace(typed) != "" {
			return strings.TrimSpace(typed)
		}
	}
	return ""
}

func nestedString(value map[string]any, path []string, fallback string) string {
	current := value
	for index, key := range path {
		if index == len(path)-1 {
			if result := firstString(current, key); result != "" {
				return result
			}
			break
		}
		current = asMap(current[key])
		if current == nil {
			break
		}
	}
	return firstString(value, fallback)
}

func firstInt(value map[string]any, keys ...string) int {
	if value == nil {
		return 0
	}
	for _, key := range keys {
		switch typed := value[key].(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case json.Number:
			parsed, _ := strconv.Atoi(typed.String())
			return parsed
		case string:
			parsed, _ := strconv.Atoi(typed)
			return parsed
		}
	}
	return 0
}

func idFromURN(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ':' || r == '(' || r == ')' || r == ','
	})
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
