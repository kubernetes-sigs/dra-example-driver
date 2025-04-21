import machine
import gc
import os
import utime
import micropython

print(micropython.mem_info())
# 1. Get CPU Frequency
cpu_freq = machine.freq()  # CPU frequency in Hz
print("CPU Frequency:", cpu_freq / 1_000_000, "MHz")

# 2. Get Unique ID (Serial Number)
unique_id = machine.unique_id()
print("Unique ID:", unique_id.hex())

# 3. Get Internal Temperature (in Celsius)
sensor_temp = machine.ADC(4)  # Internal temperature sensor
conversion_factor = 3.3 / (1 << 12)  # 12-bit ADC resolution
reading = sensor_temp.read_u16() * conversion_factor
temperature = 27 - (reading - 0.706) / 0.001721  # Formula from RP2040 datasheet
print("Temperature:", temperature, "°C")

# 4. Get Available and Used Memory
gc.collect()  # Run garbage collection to free up unused memory
free_mem = gc.mem_free()  # Get free memory in bytes
alloc_mem = gc.mem_alloc()  # Get allocated memory in bytes
print("Free Memory:", free_mem, "bytes")
print("Allocated Memory:", alloc_mem, "bytes")
print("Total RAM:", free_mem + alloc_mem, "bytes")
pi = machine.Pin("LED", machine.Pin.OUT)
pi.toggle()
utime.sleep(2)
# 5. Check GPIO Pin States (GPIO 0 to 29)
for pin in range(0, 30):  # Check GPIO 0 to 29
    try:
        p = machine.Pin(pin, machine.Pin.IN)
        print(f"GPIO {pin}: {'High' if p.value() else 'Low'}")
    except:
        pass  # Some pins may not be accessible

# 6. Get Flash Storage Information
fs_stat = os.statvfs("/")
total_blocks = fs_stat[2]
free_blocks = fs_stat[3]
block_size = fs_stat[0]

total_size = total_blocks * block_size
free_size = free_blocks * block_size
print("Total Flash Storage:", total_size, "bytes")
print("Free Flash Storage:", free_size, "bytes")

# 7. Get System Uptime
uptime = utime.ticks_ms() / 1000  # Convert milliseconds to seconds
print("System Uptime:", uptime, "seconds")

# 8. Get Reset Cause (Why Pico Restarted)
reset_cause = machine.reset_cause()

print(reset_cause)
# 9. List All Files in the Filesystem
print("\nFiles on Pico:")
print(os.listdir("/"))
#machine.bootloader()

