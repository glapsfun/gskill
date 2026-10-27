# In your project's Git repository:
gskill add github.com/glapsfun/gskill/examples/skills/hello-gskill --agent claude
git add -A && git commit -m "Add hello-gskill"

# On a fresh clone, or after deleting the installed copy:
rm -rf .agents/skills .claude/skills
gskill install --frozen-lockfile
gskill project verify
