# Proxmox + cloud-init

Ce tutoriel explique comment importer une image cloud (Arch Linux) dans Proxmox, créer un template avec cloud-init, puis déployer une VM admin-node.

---

## Prérequis

| Élément | Description |
|---------|-------------|
| Proxmox VE | ≥ 7.x avec accès SSH au nœud |
| Image cloud | Arch Linux cloud image (`.qcow2`) |
| Réseau | Un bridge configuré (ex. `vmbr0`) |
| Clé SSH | Paire ed25519 pour l'accès à la VM |

---

## Variables utilisées

| Variable | Exemple | Description |
|----------|---------|-------------|
| `PROXMOX_HOST` | `192.168.1.100` | IP ou hostname du nœud Proxmox |
| `PROXMOX_NODE` | `pve` | Nom du nœud dans le cluster |
| `PROXMOX_STORAGE` | `local-lvm` | Storage cible pour le disque |
| `ARCH_CLOUD_IMAGE` | URL vers le `.qcow2` | Image cloud Arch Linux |
| `CI_VM_BRIDGE` | `vmbr0` | Bridge réseau de la VM |
| `CI_SSH_PRIVATE_KEY` | `~/.ssh/id_ed25519` | Clé SSH d'accès admin |
| `VMID` | `9000` | ID du template (≥ 9000 recommandé) |

---

## Étape 1 — Télécharger l'image cloud

Sur le nœud Proxmox (en SSH) :

```bash
# Arch Linux cloud image (x86_64)
wget -O /tmp/arch-cloud.qcow2 \
  https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2
```

> **Astuce** : pour une autre distribution, remplacez l'URL par l'image cloud correspondante (Ubuntu, Debian, etc.).

> **Stockage** : l'image Arch actuellement validée par la CI utilise une racine Btrfs. Le paramètre Proxmox `local-lvm` désigne le stockage de l'hyperviseur pour le disque de la VM ; il ne crée pas de volumes LVM dans l'OS invité.

---

## Étape 2 — Créer la VM template

```bash
VMID=9000
STORAGE="local-lvm"
BRIDGE="vmbr0"

# Créer une VM vide
qm create $VMID --name "arch-cloud-template" \
  --memory 2048 \
  --cores 2 \
  --net0 virtio,bridge=$BRIDGE \
  --ostype l26 \
  --agent enabled=1

# Importer le disque cloud dans le storage
qm importdisk $VMID /tmp/arch-cloud.qcow2 $STORAGE

# Attacher le disque importé comme disque scsi0
qm set $VMID --scsihw virtio-scsi-pci \
  --scsi0 ${STORAGE}:vm-${VMID}-disk-0

# Configurer le boot sur le disque
qm set $VMID --boot order=scsi0

# Ajouter un lecteur cloud-init (IDE)
qm set $VMID --ide2 ${STORAGE}:cloudinit

# Activer le port série (nécessaire pour cloud-init sur certaines images)
qm set $VMID --serial0 socket --vga serial0
```

---

## Étape 3 — Configurer cloud-init dans Proxmox

```bash
# Utilisateur et clé SSH
qm set $VMID --ciuser admin
qm set $VMID --sshkeys ~/.ssh/id_ed25519.pub

# Réseau (IP statique ou DHCP)
# Option A : DHCP
qm set $VMID --ipconfig0 ip=dhcp

# Option B : IP statique
# qm set $VMID --ipconfig0 ip=192.168.1.10/24,gw=192.168.1.1
# qm set $VMID --nameserver "192.168.1.2 1.1.1.1"
```

> **Note** : pour un réseau statique avancé, utilisez le fichier `cloud-init/admin-01.network-data.example.yaml` comme référence.

---

## Étape 4 — Convertir en template

```bash
qm template $VMID
```

La VM est maintenant un template immutable. Toute création de VM se fera par clonage.

---

## Étape 5 — Déployer une VM depuis le template

```bash
NEW_VMID=101

# Clone complet du template
qm clone $VMID $NEW_VMID --name "admin-01" --full

# (Optionnel) Ajuster les ressources
qm set $NEW_VMID --memory 4096 --cores 4

# (Optionnel) Redimensionner le disque
qm resize $NEW_VMID scsi0 +18G   # total = 20G

# Personnaliser cloud-init pour cette VM
qm set $NEW_VMID --ipconfig0 ip=192.168.1.10/24,gw=192.168.1.1
qm set $NEW_VMID --nameserver "192.168.1.2 1.1.1.1"

# Démarrer la VM
qm start $NEW_VMID
```

---

## Étape 6 — Vérifier le déploiement

```bash
# Attendre ~60s que cloud-init termine, puis :
ssh admin@192.168.1.10

# Vérifier que cloud-init a terminé
cloud-init status --wait

# Vérifier les services admin-node
systemctl status admin-converge.timer
```

---

## Utilisation avec les fichiers cloud-init du repo

Ce dépôt fournit des fichiers cloud-init dans `cloud-init/` :

| Fichier | Rôle |
|---------|------|
| `admin-01.user-data.yaml` | Configuration utilisateur, paquets, scripts de convergence |
| `admin-01.network-data.example.yaml` | Exemple de configuration réseau statique |

### Injecter un user-data personnalisé

Pour utiliser le `user-data.yaml` de ce repo au lieu de la config Proxmox par défaut :

```bash
# Copier le fichier sur le nœud Proxmox
scp cloud-init/admin-01.user-data.yaml root@$PROXMOX_HOST:/var/lib/vz/snippets/

# Configurer la VM pour utiliser ce snippet
qm set $NEW_VMID --cicustom "user=local:snippets/admin-01.user-data.yaml"
```

> **Important** : le storage `local` doit avoir le type de contenu `snippets` activé.  
> Activez-le dans l'interface Proxmox : Datacenter → Storage → local → Content → cochez « Snippets ».

---

## Résumé du flux

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────┐
│ Image .qcow2    │────▶│ Template VM  │────▶│  Clone VM   │
│ (Arch cloud)    │     │ (VMID 9000)  │     │ (admin-01)  │
└─────────────────┘     └──────────────┘     └──────┬──────┘
                                                     │
                                              cloud-init
                                                     │
                                              ┌──────▼──────┐
                                              │ admin-node  │
                                              │ convergence │
                                              └─────────────┘
```

---

## Dépannage

| Problème | Solution |
|----------|----------|
| VM ne boot pas | Vérifier `qm set --boot order=scsi0` |
| Pas de réseau | Vérifier le bridge et `--ipconfig0` |
| cloud-init ne s'exécute pas | Vérifier que le lecteur `ide2` cloudinit est présent |
| SSH refused | Attendre la fin de cloud-init, vérifier la clé publique |
| Snippets non disponibles | Activer le type de contenu « Snippets » sur le storage |
