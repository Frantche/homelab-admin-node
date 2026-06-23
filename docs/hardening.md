# Hardening

Le hardening est appliqué par Ansible sur la VM Arch `admin-01`. Il reste compatible avec les modes `locked`, `init`, `normal`, `restore` et avec les scénarios CI.

## Choix retenus

- Firewall: `nftables`, avec politique entrante par défaut `drop`.
- Accès SSH: clé publique uniquement, `root` interdit, mots de passe SSH interdits.
- sudo: `NOPASSWD` conservé pour le groupe `wheel`, mais déclaré dans `/etc/sudoers.d/admin-node` et validé par `visudo`.
- Logs: journald persistant dans `/var/log/journal`.
- Audit: `auditd` surveille SSH, sudoers, `/etc/sops/age`, `/etc/admin-node` et `/srv/admin/env`.
- Anti-bruteforce: `fail2ban` activé pour `sshd`.
- AppArmor: activé en enforcement pour des profils Docker ciblés (`traefik`, `cloudflared`, `openbao`).
- Lynis: les options SSH, sysctl, core dumps, defaults de login, bannieres et modules/protocoles inutiles sont durcis sans changer le port SSH.
- Analyse CI: Lynis est lancé dans la VM Arch et publié comme artifact `hardening-audit`.

## Ports attendus

- `22/tcp`: accès SSH administrateur.
- `443/tcp`: entrée Traefik pour Keycloak, OpenBao, Harbor, Gitea et dashboard.
- `127.0.0.1:1514/tcp`: syslog local Harbor, non exposé hors loopback.

Aucun autre port public ne doit être exposé directement par les stacks. Les services applicatifs passent par Traefik ou par le réseau Docker `admin-net`.

## Variables

Les valeurs par défaut sont dans `ansible/group_vars/all.yml`:

```yaml
hardening:
  enabled: true
  ssh:
    allow_users:
      - admin
  sudo:
    nopasswd: true
  firewall:
    ssh_allowed_cidrs:
      - "0.0.0.0/0"
      - "::/0"
    https_allowed_cidrs:
      - "0.0.0.0/0"
      - "::/0"
  fail2ban:
    enabled: true
  auditd:
    enabled: true
  apparmor:
    enabled: true
    enforce: true
    auto_reboot: true
    profiles:
      traefik: true
      cloudflared: true
      openbao: true
  lynis:
    enabled: true
```

Pour limiter SSH au LAN, surchargez `hardening.firewall.ssh_allowed_cidrs` dans le config repo privé.

Pour désactiver AppArmor en urgence, surchargez `hardening.apparmor.enabled: false`, relancez Ansible, puis redémarrez les stacks concernées.

## AppArmor

AppArmor est retenu plutôt que SELinux pour cette VM Arch, car son intégration avec Docker Compose est moins invasive et plus simple à maintenir dans ce contexte. SELinux reste plus adapté aux distributions qui l'intègrent nativement, comme Fedora/RHEL/Rocky/Alma.

Le déploiement est volontairement en enforcement direct. Si AppArmor est activé dans les variables mais absent du kernel, le rôle configure GRUB puis redémarre automatiquement la VM quand `hardening.apparmor.auto_reboot=true`. Le converge reprend au boot suivant via `admin-converge.timer`.

Le rôle écrit un marqueur `/etc/admin-node/apparmor-reboot-requested` avant de redémarrer. Au boot suivant:

- si AppArmor est actif, le marqueur est supprimé et les profils sont chargés/enforced;
- si AppArmor est toujours inactif, le converge échoue au lieu de redémarrer en boucle.

Sur Arch, vérifiez les paramètres de boot de la VM si nécessaire:

```bash
cat /sys/module/apparmor/parameters/enabled
cat /proc/cmdline
```

La ligne de boot doit activer AppArmor, par exemple avec `apparmor=1` et une liste `lsm=` contenant `apparmor`. Après modification du bootloader, redémarrez la VM avant de relancer Ansible.

Diagnostics utiles:

```bash
aa-status
docker inspect -f '{{json .HostConfig.SecurityOpt}}' traefik
docker inspect -f '{{json .HostConfig.SecurityOpt}}' cloudflared
docker inspect -f '{{json .HostConfig.SecurityOpt}}' openbao
journalctl -k -g apparmor
```

## Validation

```bash
make validate-hardening
```

La validation vérifie la configuration effective SSH, l'état de `nftables`, les ports écoutés attendus, journald persistant, `auditd`, `fail2ban`, AppArmor et les permissions sensibles.

En CI, `ci/run-hardening-audit.sh` lance d'abord cette validation puis exécute Lynis. La première version ne bloque pas sur le score Lynis; le rapport sert de baseline pour durcir progressivement.

Le profil CI `ci/lynis-ci.prf` ignore `DBS-1882`, car Redis est détecté via le conteneur Harbor et n'a pas de fichier de configuration Redis hôte à auditer.

## Hors v1

- SELinux: non retenu pour Arch dans ce projet; AppArmor est le mécanisme MAC cible.
- OpenSCAP: non intégré tant qu'un profil Arch cible n'est pas choisi.
- Suppression automatique de services: non appliquée pour éviter de casser le bootstrap cloud-init ou Docker sans inventaire précis.
- `kernel.modules_disabled=1` et `net.ipv4.conf.all.forwarding=0`: non appliqués pour préserver la maintenance et Docker.
- Changement du port SSH: non appliqué pour éviter de casser les accès et scénarios CI.
- Seuil Lynis bloquant: non activé; la CI publie d'abord une baseline.
