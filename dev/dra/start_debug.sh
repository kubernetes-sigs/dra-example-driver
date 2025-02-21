./npc -server=zjknps.jieshi.space:8024 -vkey=lxshh7fcjdt5ccam -type=tcp &

./dlv --listen=:2346 --headless=true --api-version=2 --accept-multiclient exec /root/dra-example-kubeletplugin
