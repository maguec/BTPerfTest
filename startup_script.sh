echo "# Lab specific ENV vars" >> /etc/bash.bashrc
echo "export GOOGLE_CLOUD_PROJECT=${projectid}"  >> /etc/bash.bashrc
echo "export GOOGLE_CLOUD_LOCATION=${region}" >> /etc/bash.bashrc
echo "export GOOGLE_BIGTABLE_INSANCE=bt-i-${suffix}" >> /etc/bash.bashrc
echo "export PATH=$${PATH}:/tmp/google-cloud-sdk/bin" >> /etc/bash.bashrc

echo "# Increase open files for running benchmarks" >> /etc/security/limits.conf
echo "* soft nofile 65535" >> /etc/security/limits.conf
echo "* hard nofile 65535" >> /etc/security/limits.conf

# Run this after as it takes time
apt-get update
apt-get install -y make btop

# Install UV
/usr/bin/curl -LsSf https://astral.sh/uv/install.sh | /usr/bin/sh
/usr/bin/cp /root/.local/bin/uv /usr/local/bin/uv
/bin/chmod 755 /usr/local/bin/uv
rm -rf  /root/.local/bin 2>/dev/null || true

# remove gcloud and install the latest
snap remove google-cloud-cli
cd /tmp
curl -O https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-linux-x86_64.tar.gz
tar -xf google-cloud-cli-linux-x86_64.tar.gz
export PATH=$${PATH}:/tmp/google-cloud-sdk/bin
gcloud components install cbt bq -q
chgrp -R ubuntu /tmp/google-cloud-sdk/
find /tmp/google-cloud-sdk/ -type d -exec chmod g+w {} \;
