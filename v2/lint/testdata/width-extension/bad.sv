module top (input [3:0] nibble, input [7:0] byte_in, output logic [15:0] q, output logic [7:0] r);
  always_comb begin
    q = byte_in;                 // want
    r = nibble;                  // want
  end
endmodule
module logical_shift (input logic [23:0] acc, output logic [23:0] shifted);
  assign shifted = acc >> 3;  // want
endmodule
