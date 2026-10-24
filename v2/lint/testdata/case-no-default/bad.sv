module top (input [1:0] s, input [3:0] a, b, output logic [3:0] y);
  always_comb begin
    y = 0;
    case (s)                     // want
      0: y = a;
      1: y = b;
    endcase
    casez (s)                    // want
      2'b1?: y = a;
    endcase
  end
endmodule
