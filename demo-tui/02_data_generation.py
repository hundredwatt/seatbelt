import csv
import random
import string
from datetime import datetime, timedelta
from faker import Faker
from tqdm import tqdm
import multiprocessing as mp
import numpy as np
import os

fake = Faker()

def random_string(length):
    return ''.join(random.choices(string.ascii_letters + string.digits, k=length))

def generate_base_rows_chunk(args):
    start_idx, chunk_size, worker_id, output_file, lock_file = args
    rows = []
    base_time = datetime(2020, 1, 1)
    statuses = ['active', 'inactive', 'pending', 'banned']
    
    # Generate chunk data
    for i in tqdm(range(start_idx, start_idx + chunk_size),
                  desc=f"Worker {worker_id}",
                  position=worker_id + 1,
                  leave=True):
        user_id = i
        name = fake.name()
        email = fake.email()
        status = random.choice(statuses)
        created_at = base_time + timedelta(seconds=random.randint(0, 100000000))
        score = round(random.uniform(0, 1000), 2)
        description = fake.text(max_nb_chars=700)
        rows.append([
            user_id, name, email, status, created_at.isoformat(), score, description
        ])
    
    # Write chunk directly to temp file to save memory
    chunk_file = f"{output_file}.base.{worker_id}"
    with open(chunk_file, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerows(rows)
    
    # Return only the metadata and file reference, not the actual data
    return {
        "chunk_file": chunk_file,
        "count": len(rows),
        "start_idx": start_idx,
        "worker_id": worker_id
    }

def generate_and_write_base_rows(n_rows, output_file, n_workers=6):
    chunk_size = n_rows // n_workers
    remainder = n_rows % n_workers
    
    # Create a lock file for writing operations
    lock_file = f"{output_file}.lock"
    open(lock_file, 'w').close()  # Create or truncate the lock file
    
    chunks = []
    start_idx = 0
    for i in range(n_workers):
        this_chunk_size = chunk_size + (1 if i < remainder else 0)
        chunks.append((start_idx, this_chunk_size, i, output_file, lock_file))
        start_idx += this_chunk_size
    
    print(f"Generating {n_rows} base rows using {n_workers} workers...")
    
    # Create metadata file to track base chunks
    base_meta_file = f"{output_file}.base.meta"
    with open(base_meta_file, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerow(['chunk_file', 'count', 'start_idx', 'worker_id'])
    
    with mp.Pool(n_workers) as pool:
        results = list(tqdm(pool.imap(generate_base_rows_chunk, chunks),
                           total=len(chunks),
                           desc="Overall base progress",
                           position=0,
                           leave=True))
    
    # Clear progress bars
    for i in range(n_workers + 1):
        print("\033[A\033[K", end="")
    
    # Write metadata about chunks
    with open(base_meta_file, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerow(['chunk_file', 'count', 'start_idx', 'worker_id'])
        for result in results:
            writer.writerow([
                result["chunk_file"],
                result["count"],
                result["start_idx"],
                result["worker_id"]
            ])
    
    print(f"Base data generation complete. Metadata saved to {base_meta_file}")
    return base_meta_file

def process_variation_chunk(args):
    base_meta_file, repeat_indices, worker_id, output_file, lock_file = args
    
    # Read base chunk metadata
    base_chunks = []
    with open(base_meta_file, 'r', newline='', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            base_chunks.append({
                "chunk_file": row["chunk_file"],
                "count": int(row["count"]),
                "start_idx": int(row["start_idx"]),
                "worker_id": int(row["worker_id"])
            })
    
    total_variations = 0
    
    # Create a file for this worker's variations
    variation_file = f"{output_file}.var.{worker_id}"
    with open(variation_file, 'w', newline='', encoding='utf-8') as outf:
        writer = csv.writer(outf)
        
        # Process each repeat index for all base chunks
        for r in tqdm(repeat_indices,
                     desc=f"Variation worker {worker_id}",
                     position=worker_id + 1,
                     leave=True):
            
            # Process each base chunk file
            for base_chunk in base_chunks:
                # Read the base chunk
                with open(base_chunk["chunk_file"], 'r', newline='', encoding='utf-8') as inf:
                    reader = csv.reader(inf)
                    for row in reader:
                        # Generate variation
                        user_id = int(row[0]) + (r * 1_000_000)
                        name = row[1]
                        email = row[2].replace("@", f"+{r}@")
                        status = row[3]
                        created_at = datetime.fromisoformat(row[4]) + timedelta(days=r)
                        score = round(float(row[5]) + random.uniform(-5, 5), 2)
                        extra_sentences = fake.sentences(nb=random.randint(1, 3))
                        keyword = random.choice(['performance', 'latency', 'error', 'scaling', 'deployment', 'rollback', 'feature', 'hotfix'])
                        description = f"{row[6]} {'. '.join(extra_sentences)} (version {r}, keyword: {keyword})"
                        
                        # Write variation directly to file
                        writer.writerow([
                            user_id, name, email, status, created_at.isoformat(), score, description
                        ])
                        total_variations += 1
    
    return {
        "variation_file": variation_file,
        "count": total_variations,
        "worker_id": worker_id
    }

def generate_and_write_variations(base_meta_file, n_repeats, output_file, n_workers=6):
    # Create a lock file for writing operations
    lock_file = f"{output_file}.lock"
    if not os.path.exists(lock_file):
        open(lock_file, 'w').close()
    
    # Split repeats across workers
    repeat_chunks = np.array_split(range(n_repeats), n_workers)
    
    # Create work items
    work_items = []
    for i in range(n_workers):
        work_items.append((base_meta_file, repeat_chunks[i].tolist(), i, output_file, lock_file))
    
    print(f"Generating variations with {n_repeats} repeats using {n_workers} workers...")
    
    # Create metadata file
    variation_meta_file = f"{output_file}.var.meta"
    with open(variation_meta_file, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerow(['variation_file', 'count', 'worker_id'])
    
    with mp.Pool(n_workers) as pool:
        results = list(tqdm(pool.imap(process_variation_chunk, work_items),
                           total=len(work_items),
                           desc="Overall variation progress",
                           position=0,
                           leave=True))
    
    # Clear progress bars
    for i in range(n_workers + 1):
        print("\033[A\033[K", end="")
    
    # Update metadata
    with open(variation_meta_file, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerow(['variation_file', 'count', 'worker_id'])
        for result in results:
            writer.writerow([
                result["variation_file"],
                result["count"],
                result["worker_id"]
            ])
    
    print(f"Variation generation complete. Metadata saved to {variation_meta_file}")
    return variation_meta_file

def combine_to_final_file(variation_meta_file, output_file):
    print(f"Combining all variations into final output file: {output_file}")
    
    # Read variation metadata
    variation_files = []
    with open(variation_meta_file, 'r', newline='', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            variation_files.append(row["variation_file"])
    
    # Create final output file
    with open(output_file, 'w', newline='', encoding='utf-8') as outf:
        writer = csv.writer(outf)
        writer.writerow(['user_id', 'name', 'email', 'status', 'created_at', 'score', 'description'])
        
        # Copy contents of each variation file
        total_rows = 0
        for var_file in tqdm(variation_files, desc="Combining files"):
            with open(var_file, 'r', newline='', encoding='utf-8') as inf:
                reader = csv.reader(inf)
                for row in reader:
                    writer.writerow(row)
                    total_rows += 1
                    if total_rows % 100000 == 0:
                        print(f"Wrote {total_rows} rows to final file")
    
    print(f"Successfully combined all data. Wrote {total_rows} rows to {output_file}")
    
    # Clean up temporary files
    cleanup_temp_files(variation_meta_file)

def cleanup_temp_files(variation_meta_file):
    print("Cleaning up temporary files...")
    
    # Get base metadata file name
    base_meta_file = variation_meta_file.replace('.var.meta', '.base.meta')
    
    # Read and delete base chunk files
    if os.path.exists(base_meta_file):
        with open(base_meta_file, 'r', newline='', encoding='utf-8') as f:
            reader = csv.DictReader(f)
            for row in reader:
                if os.path.exists(row["chunk_file"]):
                    os.remove(row["chunk_file"])
        os.remove(base_meta_file)
    
    # Read and delete variation files
    with open(variation_meta_file, 'r', newline='', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            if os.path.exists(row["variation_file"]):
                os.remove(row["variation_file"])
    
    # Delete variation metadata
    os.remove(variation_meta_file)
    
    # Delete lock file
    lock_file = variation_meta_file.replace('.var.meta', '.lock')
    if os.path.exists(lock_file):
        os.remove(lock_file)
    
    print("Cleanup complete")

if __name__ == "__main__":
    base_count = 1_000_000  # Number of unique rows
    repeat_count = 64       # How many times to repeat with variation
    output_file = 'benchmark_data.csv'
    n_workers = 6          # Number of parallel workers
    
    # Set up environment for clean progress bars
    os.environ['PYTHONIOENCODING'] = 'utf-8'
    
    # Generate base data and write to temp files
    base_meta_file = generate_and_write_base_rows(base_count, output_file, n_workers)
    
    # Generate variations based on base data and write to temp files
    variation_meta_file = generate_and_write_variations(base_meta_file, repeat_count, output_file, n_workers)
    
    # Combine all variation files into the final output
    combine_to_final_file(variation_meta_file, output_file)
    
    print("Done.")

