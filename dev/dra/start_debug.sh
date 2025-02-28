./npc -server=39.98.113.76:8024 -vkey=lxshh7fcjdt5ccam -type=tcp &

./dlv --listen=:2346 --headless=true --api-version=2 --accept-multiclient exec /root/dra-example-kubeletplugin
