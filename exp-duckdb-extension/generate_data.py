import time
import random
import os
from xxhash import xxh32
from tqdm import tqdm
import string

# Configuration
N = 10_000_000  # Number of records
SEED0 = "DEADBEEF"
SEED1 = "CAFEBABE"

# Create output directory
os.makedirs("tmp", exist_ok=True)
file_name = f"tmp/data-{N}-{time.strftime('%Y%m%d-%H%M%S')}.txt"

def generate_random_string(length=10):
    """Generate a random string of specified length"""
    return ''.join(random.choices(string.ascii_letters + string.digits, k=length))

def generate_random_float():
    """Generate a random float between 0 and 1000"""
    return random.uniform(0, 1000)

def generate_random_date():
    """Generate a random date in YYYY-MM-DD format"""
    year = random.randint(2000, 2023)
    month = random.randint(1, 12)
    day = random.randint(1, 28)  # Simplified to avoid month-end issues
    return f"{year}-{month:02d}-{day:02d}"

time_start = time.time()

with open(file_name, "w") as f:
    for i in tqdm(range(N)):
        # Generate various types of data
        id = i + 1
        hash0 = xxh32(str(i) + SEED0).hexdigest()
        hash1 = xxh32(str(i) + SEED1).intdigest()
        mod8 = i % 8
        random_value = random.randint(0, 100)
        random_string = generate_random_string()
        random_float = generate_random_float()
        random_date = generate_random_date()
        
        # Write the record
        f.write(f"{id},{hash0},{hash1},{mod8},{random_value},{random_string},{random_float},{random_date}\n")

time_end = time.time()
print(f"Time taken: {time_end - time_start:.2f} seconds")
print(f"File saved to {file_name}")

def human_readable_size(size_bytes):
    """Convert bytes to a human-readable format (KB, MB, GB, etc.)"""
    units = ['bytes', 'KB', 'MB', 'GB', 'TB']
    size = float(size_bytes)
    unit_index = 0
    while size >= 1024 and unit_index < len(units) - 1:
        size /= 1024
        unit_index += 1
    return f"{size:.2f} {units[unit_index]}"

print(f"File size: {human_readable_size(os.path.getsize(file_name))}")