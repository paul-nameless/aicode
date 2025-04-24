Summarize the conversation to date in detail, emphasizing the user's clearly stated requests and your own previous responses. Ensure that the summary captures all key technical details, including code patterns and architectural decisions, so that future development can proceed without loss of context.

Prior to delivering your final summary, include your reasoning inside <analysis> tags. Use this section to systematically ensure you have addressed all relevant points:

1. Sequentially review each message and segment of the discussion. For each, clearly identify:
   - The user's explicit requests and intentions.
   - How you responded to each request.
   - Major decisions, technical topics, and code patterns.
   - Specifics such as file names, full code excerpts, function signatures, and any file modifications.
2. Verify the technical completeness and accuracy of your summary, making sure all required elements are thoroughly addressed.

Your summary must contain the following sections, organized as listed:

1. Primary Request and Intent: Detailed articulation of all user requests and intentions.
2. Key Technical Concepts: List of significant technologies, patterns, or frameworks discussed.
3. Files and Code Sections: Itemization of files and code sections that were viewed, edited, or created, with emphasis on recent messages. Where relevant, include full code snippets and summarize the significance of each file or change.
4. Problem Solving: Briefly document any problems solved and current troubleshooting progress.
5. Pending Tasks: Clearly list any in-progress or not-yet-addressed tasks the user has assigned.
6. Current Work: A precise and detailed account of what was being worked on immediately preceding this summary, highlighting the most recent user and assistant interactions. If applicable, include related file names and code snippets.
7. Optional Next Step: Propose the next step only if it follows directly and explicitly from the user's latest requests or the task underway before this summary. If a next step is listed, include verbatim quotes from the most recent message(s) to clarify any actions in progress and preserve intent. Do not introduce new or tangential next steps.

Here is an example of the expected structure:

<example>
[Your thought process, ensuring all points are covered thoroughly and accurately]

1. Primary Request and Intent:
   [Detailed description]

2. Key Technical Concepts:
   - [Concept 1]
   - [Concept 2]
   - [...]

3. Files and Code Sections:
   - [File Name 1]
      - [Summary of why this file is important]
      - [Summary of the changes made to this file, if any]
      - [Important Code Snippet]
   - [File Name 2]
      - [Important Code Snippet]
   - [...]

4. Problem Solving:
   [Description of solved problems and ongoing troubleshooting]

5. Pending Tasks:
   - [Task 1]
   - [Task 2]
   - [...]

6. Current Work:
   [Precise description of current work]

7. Optional Next Step:
   [Optional Next step to take]

</example>

Base your summary on the conversation up to this point in time, adhering to this structure and ensuring clarity, thoroughness, and accuracy in every section.

If there are special summarization requirements in the provided context, be sure to incorporate these as well. Common examples include:
<example>
## Compact Instructions
When summarizing the conversation focus on typescript code changes and also remember the mistakes you made and how you fixed them.
</example>

<example>
# Summary instructions
When you are using compact - please focus on test output and code changes. Include file reads verbatim.
</example>
