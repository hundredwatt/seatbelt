#define DUCKDB_EXTENSION_MAIN

#include "seatbelt_duckdb_extension.hpp"
#include "duckdb.hpp"
#include "duckdb/common/exception.hpp"
#include "duckdb/common/string_util.hpp"
#include "duckdb/function/scalar_function.hpp"
#include "duckdb/main/extension/extension_loader.hpp"
#include <duckdb/parser/parsed_data/create_scalar_function_info.hpp>
#include "duckdb/common/types/value.hpp"
#include "duckdb/common/vector_operations/vector_operations.hpp"

namespace duckdb {

// Operation enum values as UTINYINT
#define DOES_NOT_EXIST 1
#define NOOP 2
#define INSERT 3
#define UPDATE 4
#define DELETE 5
#define INSERT_AND_UPDATE 6
#define UPDATE_AND_DELETE 7
#define TRANSIENT_UPDATE 8

// For benchmarking extension UDF
inline void SeatbeltDuckdbCountDistincCharactersScalarFun(DataChunk &args, ExpressionState &state, Vector &result) {
    auto &strings_vector = args.data[0];
    UnaryExecutor::Execute<string_t, int32_t>(
	    strings_vector, result, args.size(),
	    [&](string_t name) {
            std::unordered_set<char> unique_chars;
            for (const auto &ch : name.GetString()) {
                unique_chars.insert(ch);
            }
            return unique_chars.size();
        });
}

// Modified internal function to accept raw data and null flags
static uint8_t DetermineSourceOperationInternalVarchar(bool checksum_1_null, const string_t* checksum_1_ptr,
                                                        bool checksum_0_null, const string_t* checksum_0_ptr) {
    if (checksum_1_null && checksum_0_null) {
        return DOES_NOT_EXIST;
    }
    if (!checksum_1_null && checksum_0_null) {
        return INSERT;
    }
    if (checksum_1_null && !checksum_0_null) {
        return DELETE;
    }
    // Neither is null, compare string_t directly
    if (*checksum_1_ptr != *checksum_0_ptr) {
        return UPDATE;
    }
    return NOOP;
}

// Internal function for INTEGER (int64_t) checksums
static uint8_t DetermineSourceOperationInternalInt(bool checksum_1_null, int64_t checksum_1_val,
                                                   bool checksum_0_null, int64_t checksum_0_val) {
    if (checksum_1_null && checksum_0_null) {
        return DOES_NOT_EXIST;
    }
    if (!checksum_1_null && checksum_0_null) {
        return INSERT;
    }
    if (checksum_1_null && !checksum_0_null) {
        return DELETE;
    }
    // Neither is null, compare values
    if (checksum_1_val != checksum_0_val) {
        return UPDATE;
    }
    return NOOP;
}

// Internal function for UINTEGER (uint64_t) checksums
static uint8_t DetermineSourceOperationInternalUint(bool checksum_1_null, uint64_t checksum_1_val,
                                                    bool checksum_0_null, uint64_t checksum_0_val) {
    if (checksum_1_null && checksum_0_null) {
        return DOES_NOT_EXIST;
    }
    if (!checksum_1_null && checksum_0_null) {
        return INSERT;
    }
    if (checksum_1_null && !checksum_0_null) {
        return DELETE;
    }
    // Neither is null, compare values
    if (checksum_1_val != checksum_0_val) {
        return UPDATE;
    }
    return NOOP;
}

// Modified function to iterate vectors manually - VARCHAR version
static void DetermineSourceOperationVarcharFunction(DataChunk &args, ExpressionState &state, Vector &result) {
    auto count = args.size();
    result.SetVectorType(VectorType::FLAT_VECTOR);
    auto result_data = FlatVector::GetData<uint8_t>(result);
    auto &result_validity = FlatVector::Validity(result); // We might set invalid if inputs are unexpectedly invalid

    // Ensure inputs are flat for direct data access
    args.Flatten(); 

    auto &checksum_1_vec = args.data[0];
    auto &checksum_0_vec = args.data[1];

    auto checksum_1_data = FlatVector::GetData<string_t>(checksum_1_vec);
    auto checksum_0_data = FlatVector::GetData<string_t>(checksum_0_vec);

    auto &checksum_1_validity = FlatVector::Validity(checksum_1_vec);
    auto &checksum_0_validity = FlatVector::Validity(checksum_0_vec);

    for (idx_t i = 0; i < count; ++i) {
        bool checksum_1_null = !checksum_1_validity.RowIsValid(i);
        bool checksum_0_null = !checksum_0_validity.RowIsValid(i);

        const string_t* checksum_1_ptr = checksum_1_null ? nullptr : &checksum_1_data[i];
        const string_t* checksum_0_ptr = checksum_0_null ? nullptr : &checksum_0_data[i];

        // Pass validity and data pointers to the internal function
        result_data[i] = DetermineSourceOperationInternalVarchar(checksum_1_null, checksum_1_ptr,
                                                                 checksum_0_null, checksum_0_ptr);
        // Result itself is always valid based on the internal logic's handling of nulls
        // result_validity.SetValid(i); // Default is valid, no need to set explicitly unless an error occurs
    }

     if (args.AllConstant()) {
		result.SetVectorType(VectorType::CONSTANT_VECTOR);
	}
}

// UDF wrapper for INTEGER (int64_t) checksums
static void DetermineSourceOperationIntFunction(DataChunk &args, ExpressionState &state, Vector &result) {
    auto count = args.size();
    result.SetVectorType(VectorType::FLAT_VECTOR);
    auto result_data = FlatVector::GetData<uint8_t>(result);

    args.Flatten();

    auto &checksum_1_vec = args.data[0];
    auto &checksum_0_vec = args.data[1];

    auto checksum_1_data = FlatVector::GetData<int64_t>(checksum_1_vec);
    auto checksum_0_data = FlatVector::GetData<int64_t>(checksum_0_vec);

    auto &checksum_1_validity = FlatVector::Validity(checksum_1_vec);
    auto &checksum_0_validity = FlatVector::Validity(checksum_0_vec);

    for (idx_t i = 0; i < count; ++i) {
        bool checksum_1_null = !checksum_1_validity.RowIsValid(i);
        bool checksum_0_null = !checksum_0_validity.RowIsValid(i);

        int64_t checksum_1_val = checksum_1_null ? 0 : checksum_1_data[i]; // Default value doesn't matter when null
        int64_t checksum_0_val = checksum_0_null ? 0 : checksum_0_data[i];

        result_data[i] = DetermineSourceOperationInternalInt(checksum_1_null, checksum_1_val,
                                                             checksum_0_null, checksum_0_val);
    }

    if (args.AllConstant()) {
		result.SetVectorType(VectorType::CONSTANT_VECTOR);
	}
}

// UDF wrapper for UINTEGER (uint64_t) checksums
static void DetermineSourceOperationUintFunction(DataChunk &args, ExpressionState &state, Vector &result) {
    auto count = args.size();
    result.SetVectorType(VectorType::FLAT_VECTOR);
    auto result_data = FlatVector::GetData<uint8_t>(result);

    args.Flatten();

    auto &checksum_1_vec = args.data[0];
    auto &checksum_0_vec = args.data[1];

    auto checksum_1_data = FlatVector::GetData<uint64_t>(checksum_1_vec);
    auto checksum_0_data = FlatVector::GetData<uint64_t>(checksum_0_vec);

    auto &checksum_1_validity = FlatVector::Validity(checksum_1_vec);
    auto &checksum_0_validity = FlatVector::Validity(checksum_0_vec);

    for (idx_t i = 0; i < count; ++i) {
        bool checksum_1_null = !checksum_1_validity.RowIsValid(i);
        bool checksum_0_null = !checksum_0_validity.RowIsValid(i);

        uint64_t checksum_1_val = checksum_1_null ? 0 : checksum_1_data[i]; // Default value doesn't matter when null
        uint64_t checksum_0_val = checksum_0_null ? 0 : checksum_0_data[i];

        result_data[i] = DetermineSourceOperationInternalUint(checksum_1_null, checksum_1_val,
                                                              checksum_0_null, checksum_0_val);
    }

    if (args.AllConstant()) {
		result.SetVectorType(VectorType::CONSTANT_VECTOR);
	}
}

static uint8_t DetermineDestinationOperationInternal(bool dest_present_end, bool dest_updated, bool dest_present_start) {
    if (dest_present_end && dest_updated && dest_present_start) return UPDATE;
    if (dest_present_end && dest_updated && !dest_present_start) return INSERT_AND_UPDATE;
    if (dest_present_end && !dest_updated && dest_present_start) return NOOP;
    if (dest_present_end && !dest_updated && !dest_present_start) return INSERT;
    if (!dest_present_end && dest_updated && dest_present_start) return UPDATE_AND_DELETE;
    if (!dest_present_end && dest_updated && !dest_present_start) return TRANSIENT_UPDATE;
    if (!dest_present_end && !dest_updated && dest_present_start) return DELETE;
    // if (!dest_present_end && !dest_updated && !dest_present_start)
    return DOES_NOT_EXIST;
}

static void DetermineDestinationOperationFunction(DataChunk &args, ExpressionState &state, Vector &result) {
    auto &dest_present_end_vec = args.data[0];
    auto &dest_updated_vec = args.data[1];
    auto &dest_present_start_vec = args.data[2];
    auto count = args.size();

    TernaryExecutor::Execute<bool, bool, bool, uint8_t>(
        dest_present_end_vec, dest_updated_vec, dest_present_start_vec, result, count,
        [&](bool dest_present_end, bool dest_updated, bool dest_present_start) {
            return DetermineDestinationOperationInternal(dest_present_end, dest_updated, dest_present_start);
        }
    );
}

// Template helper for comparison (needed for string_t)
template <typename T>
inline bool CompareChecksums(T v1, T v2) {
    return v1 == v2;
}

// Specialization for string_t
template <>
inline bool CompareChecksums<string_t>(string_t v1, string_t v2) {
    return v1 == v2; // string_t has operator==
}


// Templated internal function for various checksum types
template <typename SourceType, typename DestType>
static bool VerifyRowIntegrityInternalGeneric(bool inc_source_null, SourceType inc_source_val,
                                              bool inc_dest_null, DestType inc_dest_val,
                                              bool source_null, SourceType source_val,
                                              bool dest_null, DestType dest_val) {
    // If incremental checksums are null, integrity cannot be verified *incrementally*,
    // so we assume it's okay based on this check alone.
    // A separate check might be needed for overall existence/consistency later.
    if (inc_source_null || inc_dest_null) {
        return true;
    }

    // If incremental checksums are non-null, compare them to the corresponding base checksums.
    // If a base checksum is null, it's a mismatch unless the incremental value also indicates non-existence (handled implicitly by comparison).
    // Comparison needs to handle the specific types.
    bool source_match = !source_null && CompareChecksums(inc_source_val, source_val);
    bool dest_match = !dest_null && CompareChecksums(inc_dest_val, dest_val);

    return source_match && dest_match;
}


// Templated UDF wrapper for various checksum types
template <typename SourceType, typename DestType>
static void VerifyRowIntegrityGenericFunction(DataChunk &args, ExpressionState &state, Vector &result) {
    auto count = args.size();
    result.SetVectorType(VectorType::FLAT_VECTOR);
    auto result_data = FlatVector::GetData<bool>(result);
    // auto &result_validity = FlatVector::Validity(result); // Result is always valid (true/false)

    args.Flatten(); // Ensure direct data access

    // Input vectors
    auto &inc_source_vec = args.data[0];
    auto &inc_dest_vec = args.data[1];
    auto &source_vec = args.data[2];
    auto &dest_vec = args.data[3];

    // Data pointers
    auto inc_source_data = FlatVector::GetData<SourceType>(inc_source_vec);
    auto inc_dest_data = FlatVector::GetData<DestType>(inc_dest_vec);
    auto source_data = FlatVector::GetData<SourceType>(source_vec);
    auto dest_data = FlatVector::GetData<DestType>(dest_vec);

    // Validity masks
    auto &inc_source_validity = FlatVector::Validity(inc_source_vec);
    auto &inc_dest_validity = FlatVector::Validity(inc_dest_vec);
    auto &source_validity = FlatVector::Validity(source_vec);
    auto &dest_validity = FlatVector::Validity(dest_vec);

    for(idx_t i = 0; i < count; ++i) {
        // Check nulls for the current row
        bool inc_source_null = !inc_source_validity.RowIsValid(i);
        bool inc_dest_null = !inc_dest_validity.RowIsValid(i);
        bool source_null = !source_validity.RowIsValid(i);
        bool dest_null = !dest_validity.RowIsValid(i);

        // Get values (use default like 0 or "" if null, doesn't matter as null flag is checked)
        SourceType inc_source_val = inc_source_null ? SourceType() : inc_source_data[i];
        DestType inc_dest_val = inc_dest_null ? DestType() : inc_dest_data[i];
        SourceType source_val = source_null ? SourceType() : source_data[i];
        DestType dest_val = dest_null ? DestType() : dest_data[i];

        // Call the templated internal function
        result_data[i] = VerifyRowIntegrityInternalGeneric<SourceType, DestType>(
            inc_source_null, inc_source_val,
            inc_dest_null, inc_dest_val,
            source_null, source_val,
            dest_null, dest_val
        );
        // result_validity.SetValid(i); // Result is always valid boolean
    }

    if (args.AllConstant()) {
        result.SetVectorType(VectorType::CONSTANT_VECTOR);
    }
}

// Helper to register verify_row_integrity variants
template <typename SourceType, typename DestType>
void RegisterVerifyRowIntegrityVariant(ExtensionLoader &loader, const std::string &name_suffix, LogicalType source_logical_type, LogicalType dest_logical_type) {
    auto verify_func = ScalarFunction(
        "verify_row_integrity_" + name_suffix,
        {source_logical_type, dest_logical_type, source_logical_type, dest_logical_type},
        LogicalType::BOOLEAN,
        VerifyRowIntegrityGenericFunction<SourceType, DestType>, // Instantiate the template
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID,
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(verify_func);
}

static bool CheckForValidationErrorInternal(uint8_t source_op, uint8_t prev_source_op, uint8_t dest_op, uint8_t prev_dest_op, bool existing_error, bool row_verified) {
     // Rule 1: Previous source change not reflected in destination
     if ((prev_source_op != NOOP && prev_source_op != DOES_NOT_EXIST && prev_source_op != DELETE) &&
         (prev_dest_op == NOOP || prev_dest_op == DOES_NOT_EXIST) &&
         (dest_op == NOOP || dest_op == DOES_NOT_EXIST) &&
         (source_op == NOOP || source_op == DOES_NOT_EXIST)) {
         return true;
     }

     // Rule 2: Previous source DELETE not reflected in destination
     if (prev_source_op == DELETE && dest_op == NOOP) {
         return true;
     }

     // Rule 3: Row exists in source but not destination
     if ((source_op == NOOP || source_op == UPDATE) &&
         (prev_source_op == NOOP || prev_source_op == UPDATE || prev_source_op == INSERT) &&
         dest_op == DOES_NOT_EXIST) {
         return true;
     }

     // Rule 4: Row exists in destination but not source
     if (source_op == DOES_NOT_EXIST &&
         prev_source_op == DOES_NOT_EXIST &&
         dest_op == NOOP) {
         return true;
     }

     // Rule 5: Corrupted destination (data change validation)
     if (source_op == NOOP &&
         prev_source_op == NOOP &&
         dest_op != NOOP &&
         !existing_error) {
         return true;
     }

     // Rule 6: Row Corrupted (incremental checksums)
     if (source_op == NOOP && !row_verified) {
         return true;
     }

     // Rule 7: Existing error persists with no changes
     if (existing_error &&
         (source_op == NOOP || source_op == DOES_NOT_EXIST) &&
         (dest_op == NOOP || dest_op == DOES_NOT_EXIST)) {
         return true;
     }

     return false;
}

static void CheckForValidationErrorBaseFunction(DataChunk &args, ExpressionState &state, Vector &result) {
    auto count = args.size();
    result.SetVectorType(VectorType::FLAT_VECTOR);
    auto result_data = FlatVector::GetData<bool>(result);
    auto &result_validity = FlatVector::Validity(result);

    // Ensure inputs are flat for direct data access
    args.Flatten();

    auto source_op_data = FlatVector::GetData<uint8_t>(args.data[0]);
    auto prev_source_op_data = FlatVector::GetData<uint8_t>(args.data[1]);
    auto dest_op_data = FlatVector::GetData<uint8_t>(args.data[2]);
    auto prev_dest_op_data = FlatVector::GetData<uint8_t>(args.data[3]);
    auto existing_error_data = FlatVector::GetData<bool>(args.data[4]);

    auto &source_op_validity = FlatVector::Validity(args.data[0]);
    auto &prev_source_op_validity = FlatVector::Validity(args.data[1]);
    auto &dest_op_validity = FlatVector::Validity(args.data[2]);
    auto &prev_dest_op_validity = FlatVector::Validity(args.data[3]);
    auto &existing_error_validity = FlatVector::Validity(args.data[4]);


    for(idx_t i = 0; i < count; ++i) {
         // Check validity for the current row
         bool row_is_valid = source_op_validity.RowIsValid(i) &&
                             prev_source_op_validity.RowIsValid(i) &&
                             dest_op_validity.RowIsValid(i) &&
                             prev_dest_op_validity.RowIsValid(i) &&
                             existing_error_validity.RowIsValid(i);

        if (!row_is_valid) {
            result_validity.SetInvalid(i);
            continue;
        }

        auto source_op = source_op_data[i];
        auto prev_source_op = prev_source_op_data[i];
        auto dest_op = dest_op_data[i];
        auto prev_dest_op = prev_dest_op_data[i];
        auto existing_error = existing_error_data[i];

        // Default row_verified to true for the base version
        result_data[i] = CheckForValidationErrorInternal(source_op, prev_source_op, dest_op, prev_dest_op, existing_error, true);
    }

     if (args.AllConstant()) {
		result.SetVectorType(VectorType::CONSTANT_VECTOR);
	}
}

static void CheckForValidationErrorWithRowIntegrityFunction(DataChunk &args, ExpressionState &state, Vector &result) {
     auto count = args.size();
     result.SetVectorType(VectorType::FLAT_VECTOR);
     auto result_data = FlatVector::GetData<bool>(result);
     auto &result_validity = FlatVector::Validity(result);

    // Ensure inputs are flat for direct data access
    args.Flatten();

    auto source_op_data = FlatVector::GetData<uint8_t>(args.data[0]);
    auto prev_source_op_data = FlatVector::GetData<uint8_t>(args.data[1]);
    auto dest_op_data = FlatVector::GetData<uint8_t>(args.data[2]);
    auto prev_dest_op_data = FlatVector::GetData<uint8_t>(args.data[3]);
    auto existing_error_data = FlatVector::GetData<bool>(args.data[4]);
    auto row_verified_data = FlatVector::GetData<bool>(args.data[5]);

    auto &source_op_validity = FlatVector::Validity(args.data[0]);
    auto &prev_source_op_validity = FlatVector::Validity(args.data[1]);
    auto &dest_op_validity = FlatVector::Validity(args.data[2]);
    auto &prev_dest_op_validity = FlatVector::Validity(args.data[3]);
    auto &existing_error_validity = FlatVector::Validity(args.data[4]);
    auto &row_verified_validity = FlatVector::Validity(args.data[5]);

     for(idx_t i = 0; i < count; ++i) {
         // Check validity for the current row
         bool row_is_valid = source_op_validity.RowIsValid(i) &&
                            prev_source_op_validity.RowIsValid(i) &&
                            dest_op_validity.RowIsValid(i) &&
                            prev_dest_op_validity.RowIsValid(i) &&
                            existing_error_validity.RowIsValid(i) &&
                            row_verified_validity.RowIsValid(i);

        if (!row_is_valid) {
            result_validity.SetInvalid(i);
            continue;
        }

        auto source_op = source_op_data[i];
        auto prev_source_op = prev_source_op_data[i];
        auto dest_op = dest_op_data[i];
        auto prev_dest_op = prev_dest_op_data[i];
        auto existing_error = existing_error_data[i];
        auto row_verified = row_verified_data[i];

        result_data[i] = CheckForValidationErrorInternal(source_op, prev_source_op, dest_op, prev_dest_op, existing_error, row_verified);
    }

     if (args.AllConstant()) {
		result.SetVectorType(VectorType::CONSTANT_VECTOR);
	}
}

static void LoadInternal(ExtensionLoader &loader) {
    // Register seatbelt_duckdb_count_distinct_characters UDF
    auto seatbelt_duckdb_count_distinct_characters_scalar_function = ScalarFunction(
        "seatbelt_duckdb_count_distinct_characters", {LogicalType::VARCHAR},
        LogicalType::INTEGER, SeatbeltDuckdbCountDistincCharactersScalarFun,
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID, 
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(seatbelt_duckdb_count_distinct_characters_scalar_function);

    // Register determine_source_operation UDF
    // Takes two checksums (VARCHAR, allowing NULL) and returns operation (UTINYINT)
    auto determine_source_op_varchar_scalar_function = ScalarFunction(
        "determine_source_operation_varchar", {LogicalType::VARCHAR, LogicalType::VARCHAR},
        LogicalType::UTINYINT, DetermineSourceOperationVarcharFunction,
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID,
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(determine_source_op_varchar_scalar_function);

    // INTEGER (BIGINT) version
    auto determine_source_op_int_scalar_function = ScalarFunction(
        "determine_source_operation_int", {LogicalType::BIGINT, LogicalType::BIGINT},
        LogicalType::UTINYINT, DetermineSourceOperationIntFunction,
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID,
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(determine_source_op_int_scalar_function);

    // UINTEGER (UBIGINT) version
    auto determine_source_op_uint_scalar_function = ScalarFunction(
        "determine_source_operation_uint", {LogicalType::UBIGINT, LogicalType::UBIGINT},
        LogicalType::UTINYINT, DetermineSourceOperationUintFunction,
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID,
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(determine_source_op_uint_scalar_function);

    // Register determine_destination_operation UDF
    // Takes three booleans and returns operation (UTINYINT)
    auto determine_destination_operation_scalar_function = ScalarFunction(
        "determine_destination_operation", {LogicalType::BOOLEAN, LogicalType::BOOLEAN, LogicalType::BOOLEAN},
        LogicalType::UTINYINT, DetermineDestinationOperationFunction,
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID, 
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(determine_destination_operation_scalar_function);

    // --- Register verify_row_integrity variants using template helper ---
    RegisterVerifyRowIntegrityVariant<string_t, string_t>(loader, "varchar", LogicalType::VARCHAR, LogicalType::VARCHAR); // V, V
    RegisterVerifyRowIntegrityVariant<int64_t, int64_t>(loader, "int", LogicalType::BIGINT, LogicalType::BIGINT);       // I, I
    RegisterVerifyRowIntegrityVariant<uint64_t, uint64_t>(loader, "uint", LogicalType::UBIGINT, LogicalType::UBIGINT);    // U, U

    RegisterVerifyRowIntegrityVariant<string_t, int64_t>(loader, "v_i", LogicalType::VARCHAR, LogicalType::BIGINT);     // V, I
    RegisterVerifyRowIntegrityVariant<string_t, uint64_t>(loader, "v_u", LogicalType::VARCHAR, LogicalType::UBIGINT);    // V, U

    RegisterVerifyRowIntegrityVariant<int64_t, string_t>(loader, "i_v", LogicalType::BIGINT, LogicalType::VARCHAR);     // I, V
    RegisterVerifyRowIntegrityVariant<int64_t, uint64_t>(loader, "i_u", LogicalType::BIGINT, LogicalType::UBIGINT);     // I, U

    RegisterVerifyRowIntegrityVariant<uint64_t, string_t>(loader, "u_v", LogicalType::UBIGINT, LogicalType::VARCHAR);     // U, V
    RegisterVerifyRowIntegrityVariant<uint64_t, int64_t>(loader, "u_i", LogicalType::UBIGINT, LogicalType::BIGINT);     // U, I

    // Register check_for_validation_error (base version) UDF
    // Takes four operations (UTINYINT) and existing error (BOOLEAN), returns BOOLEAN
    auto check_validation_error_base_scalar_function = ScalarFunction(
        "check_for_validation_error_base", {LogicalType::UTINYINT, LogicalType::UTINYINT, LogicalType::UTINYINT, LogicalType::UTINYINT, LogicalType::BOOLEAN},
        LogicalType::BOOLEAN, CheckForValidationErrorBaseFunction,
        nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID, 
        FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(check_validation_error_base_scalar_function);

    // Register check_for_validation_error_with_row_integrity UDF
    // Takes four operations (UTINYINT), existing error (BOOLEAN), row verified (BOOLEAN), returns BOOLEAN
     auto check_validation_error_integrity_scalar_function = ScalarFunction(
         "check_for_validation_error_with_row_integrity", {LogicalType::UTINYINT, LogicalType::UTINYINT, LogicalType::UTINYINT, LogicalType::UTINYINT, LogicalType::BOOLEAN, LogicalType::BOOLEAN},
         LogicalType::BOOLEAN, CheckForValidationErrorWithRowIntegrityFunction,
         nullptr, nullptr, nullptr, nullptr, LogicalType::INVALID, 
         FunctionStability::CONSISTENT, FunctionNullHandling::SPECIAL_HANDLING);
    loader.RegisterFunction(check_validation_error_integrity_scalar_function);
}

void SeatbeltDuckdbExtension::Load(ExtensionLoader &loader) {
	LoadInternal(loader);
}
std::string SeatbeltDuckdbExtension::Name() {
	return "seatbelt_duckdb";
}

std::string SeatbeltDuckdbExtension::Version() const {
#ifdef EXT_VERSION_SEATBELT_DUCKDB
	return EXT_VERSION_SEATBELT_DUCKDB;
#else
	return "";
#endif
}

} // namespace duckdb

extern "C" {

DUCKDB_CPP_EXTENSION_ENTRY(seatbelt_duckdb, loader) {
	duckdb::LoadInternal(loader);
}
}

#ifndef DUCKDB_EXTENSION_MAIN
#error DUCKDB_EXTENSION_MAIN not defined
#endif
