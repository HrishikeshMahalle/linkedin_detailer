const form = document.querySelector("#profile-form");
const submitButton = document.querySelector("#submit-button");
const statusLine = document.querySelector("#status");
const profileCard = document.querySelector("#profile-card");
const profileImage = document.querySelector("#profile-image");
const profileName = document.querySelector("#profile-name");
const profileHeadline = document.querySelector("#profile-headline");
const profileLocation = document.querySelector("#profile-location");
const profileSections = document.querySelector("#profile-sections");
const jsonPanel = document.querySelector("#json-panel");
const jsonOutput = document.querySelector("#json-output");
const copyButton = document.querySelector("#copy-button");

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  clearResult();
  setStatus("Fetching profile…", "loading");
  submitButton.disabled = true;

  const url = document.querySelector("#profile-url").value.trim();
  const apiKey = document.querySelector("#api-key").value;
  const liAt = document.querySelector("#li-at").value.trim();
  const jsessionId = document.querySelector("#jsession-id").value.trim();
  const headers = { "Content-Type": "application/json" };
  if (apiKey) {
    headers["X-API-Key"] = apiKey;
  }

  try {
    const response = await fetch("/api/v1/profiles", {
      method: "POST",
      headers,
      body: JSON.stringify({
        url,
        linkedin_session: {
          li_at: liAt,
          jsession_id: jsessionId,
        },
      }),
    });
    const body = await response.json().catch(() => null);
    if (!response.ok) {
      const message = body?.error?.message || `Request failed with HTTP ${response.status}.`;
      throw new Error(message);
    }
    renderResult(body);
    const source = body.meta?.cache_hit ? "cache" : "LinkedIn";
    const warningCount = body.meta?.warnings?.length || 0;
    setStatus(
      warningCount ? `Profile loaded from ${source} with ${warningCount} warning(s).` : `Profile loaded from ${source}.`,
      warningCount ? "warning" : "success",
    );
  } catch (error) {
    setStatus(error instanceof Error ? error.message : "The request failed.", "error");
  } finally {
    submitButton.disabled = false;
  }
});

copyButton.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(jsonOutput.textContent);
    copyButton.textContent = "Copied";
    setTimeout(() => {
      copyButton.textContent = "Copy";
    }, 1500);
  } catch {
    setStatus("Could not copy the response.", "error");
  }
});

function renderResult(result) {
  const data = result.profile || {};
  profileName.textContent = data.full_name || "Unnamed profile";
  profileHeadline.textContent = data.headline || "";
  profileLocation.textContent = data.location || "";

  const image = largestImage(data.profile_images);
  if (image && isAllowedImageURL(image.url)) {
    profileImage.src = image.url;
    profileImage.alt = `${data.full_name || "Profile"} picture`;
    profileImage.hidden = false;
  }

  if (data.about) {
    appendTextSection("About", data.about);
  }
  appendExperience(data.experience || []);
  appendEducation(data.education || []);
  appendSkills(data.skills || []);
  appendCertifications(data.certifications || []);
  appendLanguages(data.languages || []);

  profileCard.hidden = false;
  jsonOutput.textContent = JSON.stringify(result, null, 2);
  jsonPanel.hidden = false;
}

function appendTextSection(title, text) {
  const section = sectionElement(title);
  const paragraph = document.createElement("p");
  paragraph.className = "body-copy";
  paragraph.textContent = text;
  section.append(paragraph);
  profileSections.append(section);
}

function appendExperience(entries) {
  if (!entries.length) return;
  const section = sectionElement("Experience");
  entries.forEach((entry) => {
    const item = document.createElement("article");
    item.className = "list-item";
    appendHeading(item, entry.company_name || "Company");
    (entry.positions || []).forEach((position) => {
      const block = document.createElement("div");
      block.className = "position";
      appendStrong(block, position.title || "Position");
      appendMeta(block, [position.employment_type, formatRange(position.date_range), position.location]);
      appendDescription(block, position.description);
      item.append(block);
    });
    section.append(item);
  });
  profileSections.append(section);
}

function appendEducation(entries) {
  if (!entries.length) return;
  const section = sectionElement("Education");
  entries.forEach((entry) => {
    const item = document.createElement("article");
    item.className = "list-item";
    appendHeading(item, entry.school_name || "School");
    appendStrong(item, [entry.degree_name, entry.field_of_study].filter(Boolean).join(", "));
    appendMeta(item, [formatRange(entry.date_range), entry.grade]);
    appendDescription(item, entry.description);
    section.append(item);
  });
  profileSections.append(section);
}

function appendSkills(entries) {
  if (!entries.length) return;
  const section = sectionElement("Skills");
  const tags = document.createElement("div");
  tags.className = "tags";
  entries.forEach((entry) => {
    const tag = document.createElement("span");
    tag.textContent = entry.name;
    tags.append(tag);
  });
  section.append(tags);
  profileSections.append(section);
}

function appendCertifications(entries) {
  if (!entries.length) return;
  const section = sectionElement("Certifications");
  entries.forEach((entry) => {
    const item = document.createElement("article");
    item.className = "list-item";
    appendHeading(item, entry.name || "Certification");
    appendMeta(item, [entry.issuing_organization, formatPartialDate(entry.issue_date)]);
    section.append(item);
  });
  profileSections.append(section);
}

function appendLanguages(entries) {
  if (!entries.length) return;
  const section = sectionElement("Languages");
  entries.forEach((entry) => {
    const item = document.createElement("p");
    item.className = "language";
    item.textContent = entry.proficiency ? `${entry.name} — ${entry.proficiency}` : entry.name;
    section.append(item);
  });
  profileSections.append(section);
}

function sectionElement(title) {
  const section = document.createElement("section");
  section.className = "profile-section";
  const heading = document.createElement("h3");
  heading.textContent = title;
  section.append(heading);
  return section;
}

function appendHeading(parent, text) {
  if (!text) return;
  const heading = document.createElement("h4");
  heading.textContent = text;
  parent.append(heading);
}

function appendStrong(parent, text) {
  if (!text) return;
  const strong = document.createElement("strong");
  strong.textContent = text;
  parent.append(strong);
}

function appendMeta(parent, values) {
  const text = values.filter(Boolean).join(" · ");
  if (!text) return;
  const meta = document.createElement("p");
  meta.className = "muted";
  meta.textContent = text;
  parent.append(meta);
}

function appendDescription(parent, text) {
  if (!text) return;
  const paragraph = document.createElement("p");
  paragraph.className = "body-copy";
  paragraph.textContent = text;
  parent.append(paragraph);
}

function formatRange(range) {
  if (!range) return "";
  const start = formatPartialDate(range.start);
  const end = range.is_current ? "Present" : formatPartialDate(range.end);
  return [start, end].filter(Boolean).join(" – ");
}

function formatPartialDate(date) {
  if (!date?.year) return "";
  if (!date.month) return String(date.year);
  const month = new Intl.DateTimeFormat("en", { month: "short" }).format(new Date(2000, date.month - 1, 1));
  return `${month} ${date.year}`;
}

function largestImage(images) {
  if (!Array.isArray(images) || !images.length) return null;
  return [...images].sort((a, b) => (b.width || 0) * (b.height || 0) - (a.width || 0) * (a.height || 0))[0];
}

function isAllowedImageURL(value) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "https:" && (parsed.hostname === "licdn.com" || parsed.hostname.endsWith(".licdn.com"));
  } catch {
    return false;
  }
}

function clearResult() {
  profileCard.hidden = true;
  jsonPanel.hidden = true;
  profileSections.replaceChildren();
  profileImage.hidden = true;
  profileImage.removeAttribute("src");
}

function setStatus(message, state) {
  statusLine.textContent = message;
  statusLine.dataset.state = state;
}
