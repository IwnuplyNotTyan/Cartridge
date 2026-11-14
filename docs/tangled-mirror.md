# Tangled mirror

GitHub is the primary forge. Tangled (`iwnuply.tngl.sh/Cartridge`) is a
read-only mirror of the GitHub repository.

## What is mirrored

`.github/workflows/tangled-mirror.yml` pushes:

- `refs/heads/*` — every branch;
- `refs/tags/*` — every tag.

The push uses `--force --prune`, so a branch or tag deleted on GitHub is also
deleted on Tangled. The workflow runs on every push, on branch/tag deletion,
and on manual dispatch. It is serialized, so two mirror runs never race.

`actions/checkout` creates a local branch only for the checked-out ref. Other
branches stay in `refs/remotes/origin/*`, which is why the workflow pushes that
namespace to `refs/heads/*` on Tangled.

## Fork protection

The job is skipped unless both conditions are true:

```yaml
github.repository == 'IwnuplyNotTyan/Cartridge' &&
github.event.repository.fork == false
```

A fork contains the workflow file, but the job never starts there: it does not
read the secret and does not contact Tangled. GitHub also does not expose
repository secrets to fork workflows.

## One-time setup

1. Generate a dedicated SSH key (or use the one already on disk):

   ```bash
   ssh-keygen -q -t ed25519 -N '' \
     -C 'cartridge-tangled-mirror (github actions)' \
     -f "$HOME/.ssh/cartridge-tangled-mirror"
   ```

2. Add the **public** key to the Tangled account that owns the repository:

   ```bash
   cat "$HOME/.ssh/cartridge-tangled-mirror.pub"
   ```

   Tangled: account settings → SSH keys → add the printed line.

3. Store the **private** key as a GitHub Actions secret:

   ```bash
   gh secret set TANGLED_SSH_KEY \
     --repo IwnuplyNotTyan/Cartridge \
     < "$HOME/.ssh/cartridge-tangled-mirror"
   ```

The private key stays at `~/.ssh/cartridge-tangled-mirror` (mode `600`) and in
the encrypted GitHub secret. It must never be committed to the repository.

## Manual sync

```bash
gh workflow run tangled-mirror.yml --repo IwnuplyNotTyan/Cartridge
gh run list --repo IwnuplyNotTyan/Cartridge \
  --workflow tangled-mirror.yml --limit 1
```

## Troubleshooting

- **`TANGLED_SSH_KEY is not configured`** — add the repository secret with the
  command above.
- **`Permission denied (publickey)`** — the public key is not registered on
  Tangled, or the secret contains the wrong private key.
- **`Host key verification failed`** — Tangled rotated its SSH host key.
  Verify the new key with `ssh-keyscan -t ed25519 tangled.org`, compare it with
  Tangled's published information, and update the pinned line in the workflow.
- **A branch or tag is missing on Tangled** — run the workflow manually. The
  next normal push repairs the mirror as well.
