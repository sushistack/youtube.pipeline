# Stage 4: Fact and Consistency Review

You are reviewing a generated scenario package for factual accuracy and visual consistency.

Return JSON only.
Do not use markdown fences.

## Source Facts
{scp_fact_sheet}

## Narration Script
{narration_script}

## Visual Breakdown
{visual_descriptions}

## Frozen Descriptor
{scp_visual_reference}

## Format Reference
{format_guide}

## Review Focus

Check:
- factual accuracy against source facts
- missing critical facts from the narration
- Frozen Descriptor consistency inside visual descriptors
- invented or non-canonical visual content
- shot-count and transition sanity
- cross-scene consistency problems

Do not score storytelling quality.
Do not output rubric commentary.

## Required JSON Shape

{
  "overall_pass": true,
  "coverage_pct": 100.0,
  "issues": [
    {
      "scene_num": 1,
      "type": "fact_error",
      "severity": "critical",
      "description": "Describe the problem.",
      "correction": "Describe the correction."
    }
  ],
  "corrections": [
    {
      "scene_num": 1,
      "field": "visual_descriptor",
      "original": "original text",
      "corrected": "corrected text"
    }
  ],
  "reviewer_model": "",
  "reviewer_provider": "",
  "source_version": "v1-reviewer-fact-check"
}

Rules:
- `type` must be one of `fact_error`, `missing_fact`, `descriptor_violation`, `invented_content`, `consistency_issue`
- `severity` must be one of `critical`, `warning`, `info`
- `field` must be either `narration` or `visual_descriptor`
- if there are no issues, return empty arrays
- keep corrections specific and minimally scoped
