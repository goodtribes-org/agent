package board

// The GraphQL documents. They live together so the shape of what the workers
// ask GitHub for is readable in one place.

// qProjectMeta resolves the project node id, the Status field id and every
// single-select option id on it.
//
// fields(first:20) is enough for this board (17 fields) and is checked: if the
// Status field is not in the page, discovery fails loudly rather than
// silently driving a board it cannot see.
const qProjectMeta = `
query($org: String!, $number: Int!) {
  organization(login: $org) {
    projectV2(number: $number) {
      id
      title
      fields(first: 20) {
        nodes {
          ... on ProjectV2FieldCommon { id name }
          ... on ProjectV2SingleSelectField {
            id
            name
            options { id name }
          }
        }
      }
    }
  }
}`

// qProjectItems pages through the board. Every field value the selector needs
// comes back in one round trip; fetching them per item would turn one query
// into a hundred.
//
// Only Issue content is asked for. Draft items and pull requests come back
// with an empty content object and are skipped by the flattener.
const qProjectItems = `
query($org: String!, $number: Int!, $first: Int!, $after: String) {
  organization(login: $org) {
    projectV2(number: $number) {
      items(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          isArchived
          fieldValues(first: 20) {
            nodes {
              ... on ProjectV2ItemFieldTextValue {
                text
                field { ... on ProjectV2FieldCommon { name } }
              }
              ... on ProjectV2ItemFieldSingleSelectValue {
                name
                field { ... on ProjectV2FieldCommon { name } }
              }
              ... on ProjectV2ItemFieldNumberValue {
                number
                field { ... on ProjectV2FieldCommon { name } }
              }
            }
          }
          content {
            ... on Issue {
              id
              number
              title
              url
              state
              repository { name owner { login } }
              labels(first: 20) { nodes { name } }
            }
          }
        }
      }
    }
  }
}`

// qIssue reads one issue in full, including the comment tail the stages scan
// for sentinels. comments(last:) is deliberate — the sentinel that matters is
// always the most recent one, and the head of a long thread is noise.
const qIssue = `
query($owner: String!, $repo: String!, $number: Int!, $comments: Int!) {
  repository(owner: $owner, name: $repo) {
    issue(number: $number) {
      id
      number
      title
      body
      url
      state
      labels(first: 20) { nodes { name } }
      comments(last: $comments) {
        nodes {
          id
          body
          createdAt
          author { login }
        }
      }
    }
  }
}`

// mAddComment posts a comment on an issue.
const mAddComment = `
mutation($subjectId: ID!, $body: String!) {
  addComment(input: {subjectId: $subjectId, body: $body}) {
    commentEdge { node { id } }
  }
}`

// mSetStatus moves a card between columns. This is the only mutation that
// changes the board, and internal/board/status.go guards which target values
// it will accept.
const mSetStatus = `
mutation($project: ID!, $item: ID!, $field: ID!, $option: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $project,
    itemId: $item,
    fieldId: $field,
    value: { singleSelectOptionId: $option }
  }) {
    projectV2Item { id }
  }
}`

// qRepoContext reads the repository description, default branch and README in
// one call. The tree comes from the REST git/trees endpoint instead, because
// GraphQL has no recursive tree query.
const qRepoContext = `
query($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) {
    description
    defaultBranchRef { name }
  }
}`
