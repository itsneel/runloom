interface OrganizationChoice {
  id: string;
  name: string;
  parent_id?: string;
}
export function organizationOptions(
  organizations: OrganizationChoice[],
  selectedID: string,
): Array<{ id: string; label: string }> {
  const selected = organizations.find((org) => org.id === selectedID);
  if (!selected) return [];
  const familyID = selected.parent_id || selected.id;
  const byID = new Map(organizations.map((org) => [org.id, org]));
  const rootName = (org: OrganizationChoice) => byID.get(org.parent_id || '')?.name || org.name;
  return [...organizations]
    .filter((org) => org.id === familyID || org.parent_id === familyID)
    .sort(
      (a, b) =>
        rootName(a).localeCompare(rootName(b)) ||
        Number(!!a.parent_id) - Number(!!b.parent_id) ||
        a.name.localeCompare(b.name),
    )
    .map((org) => ({ id: org.id, label: org.parent_id ? `↳ ${org.name} (child)` : org.name }));
}
